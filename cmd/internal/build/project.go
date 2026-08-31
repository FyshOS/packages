package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// ProjectOptions describes a project that builds and installs itself with a
// makefile, rather than being a single Fyne application.
//
// The makefile's install target is the manifest: whatever it puts under
// DESTDIR becomes the package payload, so binaries, desktop entries, systemd
// units and session files all come along without being listed twice.
type ProjectOptions struct {
	SourceDir string
	OutputDir string

	Arches []string // Debian architectures to build

	Name        string
	Version     string // default: derived from git describe
	Section     string
	Priority    string
	Maintainer  string
	Description string
	Homepage    string
	Prefix      string // install prefix, default /usr

	// Depends is used when dependencies cannot be worked out from the built
	// binaries. Normally dpkg-shlibdeps does that job inside the container.
	Depends []string
	// BuildDepends are the Debian packages the build needs. golang-go, make
	// and dpkg-dev are always installed and need not be listed.
	BuildDepends []string

	MakeTargets   []string // default: build
	InstallTarget string   // default: install
	Image         string   // override the base image for every architecture

	// AllowSudo permits a rootful container for an architecture that cannot
	// build any other way. It is permission, not instruction: architectures
	// that build rootless still do, and only the podman command is elevated,
	// never the rest of the build.
	AllowSudo bool

	Log func(format string, args ...any)
}

// buildImage is the base image for each Debian architecture, along with the
// podman --arch value. Trixie dropped i386, so those builds use bookworm, the
// same base the 32bit ISO is built from.
type buildImage struct {
	podmanArch string
	image      string
	backports  string // suite to pull golang-go from, when the base is too old
}

var buildImages = map[string]buildImage{
	"amd64": {podmanArch: "amd64", image: "docker.io/library/debian:trixie"},
	"arm64": {podmanArch: "arm64", image: "docker.io/library/debian:trixie"},
	"i386":  {podmanArch: "386", image: "docker.io/library/debian:bookworm", backports: "bookworm-backports"},
}

// qemuHandler names the binfmt_misc handler that lets this machine execute
// binaries of a foreign architecture.
var qemuHandler = map[string]string{
	"amd64": "qemu-x86_64",
	"arm64": "qemu-aarch64",
	"armhf": "qemu-arm",
	"i386":  "qemu-i386",
}

// checkEmulation works out how a container of the given architecture has to be
// run here, and reports whether that one needs to be rootful. The build happens
// inside the container as that architecture, so without a usable emulator the
// container's own shell will not start and podman fails with a bare
// "Exec format error".
func checkEmulation(arch string, allowSudo bool) (bool, error) {
	host, err := hostArch()
	if err != nil {
		return false, err
	}
	// A machine runs its own architecture, and x86-64 executes i386 directly.
	if arch == host || (host == "amd64" && arch == "i386") {
		return false, nil
	}

	handler, ok := qemuHandler[arch]
	if !ok {
		return false, fmt.Errorf("no emulator is known for the %s architecture", arch)
	}
	if _, err := os.Stat(filepath.Join("/proc/sys/fs/binfmt_misc", handler)); err != nil {
		return false, fmt.Errorf(`cannot build %s on %s: the %s binfmt handler is not registered,
  so %s containers cannot start. Install the emulators:
    sudo apt install qemu-user-static
  or register them until the next reboot:
    podman run --rm --privileged docker.io/multiarch/qemu-user-static --reset -p yes
  Build only what this machine can run with, for example, -arch %s`,
			arch, host, handler, arch, host)
	}

	// The handler exists, but that is not enough: on kernels where binfmt_misc
	// is per user namespace a rootless container gets an empty one and the
	// foreign binary fails to exec. Cheaper to find out with a throwaway
	// container than after a pull and a build.
	probe := exec.Command("podman", "run", "--rm", "--arch="+podmanArch(arch),
		buildImages[arch].image, "true")
	if err := probe.Run(); err == nil {
		return false, nil
	}

	if allowSudo {
		return true, nil
	}
	return false, fmt.Errorf(`cannot build %s on %s: rootless containers cannot use the
  %s emulator, because binfmt handlers do not reach their user namespace.
  Allow this one architecture's container to run rootful, which does not use
  a user namespace and so can be emulated:
    make repo FYSHPKG_FLAGS="-sudo"
  That elevates the podman command for %s alone - every other architecture
  still builds rootless, and nothing else in the build runs as root.
  Or leave it out: -arch %s`,
		arch, host, handler, arch, host)
}

// podmanArch is the name podman knows an architecture by.
func podmanArch(arch string) string {
	if img, ok := buildImages[arch]; ok {
		return img.podmanArch
	}
	return arch
}

// Project builds the makefile project in opts.SourceDir once per architecture,
// each inside a container of that architecture, and wraps each result as a
// .deb. Nothing is cross-compiled: the container runs as the target
// architecture, so cgo, pkg-config and the system headers all behave normally.
func Project(opts ProjectOptions) ([]*Result, error) {
	logf := opts.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}

	source, err := filepath.Abs(opts.SourceDir)
	if err != nil {
		return nil, err
	}
	if len(opts.Arches) == 0 {
		return nil, fmt.Errorf("no architectures to build")
	}
	if _, err := exec.LookPath("podman"); err != nil {
		return nil, fmt.Errorf("podman is not installed: apt install podman")
	}

	spec, number, err := resolveProject(source, opts)
	if err != nil {
		return nil, err
	}

	// Work out how each architecture has to run before pulling an image or
	// spending a build on it.
	rootful := make(map[string]bool, len(opts.Arches))
	for _, arch := range opts.Arches {
		needsRoot, err := checkEmulation(arch, opts.AllowSudo)
		if err != nil {
			return nil, err
		}
		rootful[arch] = needsRoot
	}

	work, err := os.MkdirTemp("", "fyshpkg-project-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)

	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return nil, err
	}

	var results []*Result
	for _, arch := range opts.Arches {
		img, ok := buildImages[arch]
		if !ok {
			return nil, fmt.Errorf("no build image for the %s architecture", arch)
		}

		how := ""
		if rootful[arch] {
			how = ", in a rootful container so it can be emulated"
		}
		logf("building %s %s for %s in %s%s", spec.Name, spec.Version, arch, img.image, how)
		staging := filepath.Join(work, arch, "pkg")
		meta := filepath.Join(work, arch, "meta")
		if err := runContainer(source, staging, meta, arch, img, spec, opts, rootful[arch]); err != nil {
			return nil, fmt.Errorf("%s: %w", arch, err)
		}

		installed, err := stagedFiles(staging)
		if err != nil {
			return nil, err
		}
		if len(installed) == 0 {
			return nil, fmt.Errorf("%s: the install target put no files under DESTDIR", arch)
		}

		archSpec := *spec
		archSpec.Arch = arch
		if found := readDepends(filepath.Join(meta, "depends")); len(found) > 0 {
			archSpec.Depends = found
		}

		if err := writeControl(staging, &archSpec, installed); err != nil {
			return nil, err
		}
		out := filepath.Join(opts.OutputDir, debFileName(&archSpec))
		if err := runDpkgDeb(staging, out); err != nil {
			return nil, err
		}

		logf("packaged %s %s (%s) from %d file(s)", archSpec.Name, archSpec.Version, arch, len(installed))
		results = append(results, &Result{
			Path: out, Name: archSpec.Name, Version: archSpec.Version, Arch: arch, Build: number,
		})
	}

	// Record the build number only once every architecture is packaged, so a
	// release that fell over halfway does not consume one.
	if number > 0 {
		if err := setBuildNumber(source, number); err != nil {
			logf("could not record build %d in %s: %v", number, MetadataName, err)
		}
	}
	return results, nil
}

// resolveProject fills in the control fields, and returns the build number to
// record afterwards, or zero when the version did not come from FyneApp.toml.
func resolveProject(source string, opts ProjectOptions) (*spec, int, error) {
	s := &spec{
		Name:        opts.Name,
		Version:     opts.Version,
		Section:     opts.Section,
		Priority:    opts.Priority,
		Maintainer:  opts.Maintainer,
		Depends:     opts.Depends,
		Description: opts.Description,
		Homepage:    opts.Homepage,
		Prefix:      opts.Prefix,
	}

	if s.Name == "" {
		s.Name = DebianName(filepath.Base(source))
	}

	// A project with FyneApp.toml is versioned from it, the same way a single
	// Fyne application is, so everything in the archive reads alike. Only a
	// project without one falls back to its position in git history.
	number := 0
	app, appErr := LoadMetadata(source)
	if appErr == nil && app.Details.Version != "" {
		number = app.Details.Build + 1
		if s.Version == "" {
			s.Version = DebianVersion(app.Details.Version, number)
		}
		if s.Description == "" {
			s.Description = app.Synopsis()
		}
		if s.Homepage == "" {
			s.Homepage = app.Website
		}
	}
	if s.Version == "" {
		version, err := GitVersion(source)
		if err != nil {
			return nil, 0, fmt.Errorf("could not derive a version: %w — pass -version", err)
		}
		s.Version = version
	}
	if opts.Version != "" {
		number = 0 // an explicit version owns the numbering
	}
	if s.Description == "" {
		s.Description = s.Name
	}
	if s.Section == "" {
		s.Section = "misc"
	}
	if s.Priority == "" {
		s.Priority = "optional"
	}
	if s.Prefix == "" {
		s.Prefix = "/usr"
	}

	if !validName.MatchString(s.Name) {
		return nil, 0, fmt.Errorf("%q is not a valid Debian package name — set one with -name", s.Name)
	}
	if !validVersion.MatchString(s.Version) {
		return nil, 0, fmt.Errorf("%q is not a valid Debian version — set one with -version", s.Version)
	}
	if s.Maintainer == "" {
		return nil, 0, fmt.Errorf("no maintainer set — add \"maintainer\" to the package section of repo.json, or pass -maintainer")
	}
	return s, number, nil
}

var describeRE = regexp.MustCompile(`^(.*)-([0-9]+)-(g[0-9a-f]+)$`)

// GitVersion turns the repository's position in history into a Debian version.
//
// On a tag that is the tag itself, so v0.4.0 becomes 0.4.0-1. Past a tag the
// commit count and hash are appended after a "+", as in 0.4.0+393.g5f1194d5-1,
// which apt orders after the tagged release and before the next one.
func GitVersion(dir string) (string, error) {
	cmd := exec.Command("git", "describe", "--tags", "--always")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git describe failed (no tags?)")
	}

	describe := strings.TrimSpace(string(out))
	describe = strings.TrimPrefix(describe, "v")
	if m := describeRE.FindStringSubmatch(describe); m != nil {
		describe = fmt.Sprintf("%s+%s.%s", strings.TrimPrefix(m[1], "v"), m[2], m[3])
	}
	return describe + "-1", nil
}

// readDepends loads the dependency line dpkg-shlibdeps produced in the
// container, if it managed to produce one.
func readDepends(name string) []string {
	raw, err := os.ReadFile(name)
	if err != nil {
		return nil
	}
	var out []string
	for _, dep := range strings.Split(strings.TrimSpace(string(raw)), ",") {
		if dep = strings.TrimSpace(dep); dep != "" {
			out = append(out, dep)
		}
	}
	return out
}

// stagedFiles lists the payload the install target produced, relative to the
// staging directory.
func stagedFiles(staging string) ([]string, error) {
	var out []string
	err := filepath.Walk(staging, func(name string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(staging, name)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out, err
}
