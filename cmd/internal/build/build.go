package build

import (
	"archive/tar"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ulikunitz/xz"
)

// Options controls how a source directory becomes a set of .deb files, one per
// architecture. Anything left empty is taken from FyneApp.toml or falls back
// to a documented default.
type Options struct {
	SourceDir string // application source, holding FyneApp.toml
	OutputDir string // where the .deb files are written

	Arches []string // Debian architectures to build, e.g. amd64 and arm64

	Name        string // Debian package name
	Version     string // Debian version, including revision
	Section     string
	Priority    string
	Maintainer  string
	Depends     []string
	Description string
	Homepage    string
	Prefix      string // install prefix, default /usr

	Release bool   // pass --release to fyne-cross
	Tags    string // build tags

	// Log receives progress lines. Nil discards them.
	Log func(format string, args ...any)
}

// Result describes one package that was built.
type Result struct {
	Path    string
	Name    string
	Version string
	Arch    string
	Build   int
}

// crossArch maps a Debian architecture onto the name fyne-cross uses for it.
var crossArch = map[string]string{
	"amd64": "amd64",
	"arm64": "arm64",
	"i386":  "386",
	"armhf": "arm",
}

// Package cross-builds the application in opts.SourceDir for every requested
// architecture and returns one .deb per architecture.
//
// fyne-cross compiles each architecture in a container and lays out the
// binary, desktop entry and icon; that layout is restaged under /usr (fyne
// targets /usr/local, which Debian policy reserves for the local
// administrator) and handed to dpkg-deb.
func Package(opts Options) ([]*Result, error) {
	logf := opts.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}

	// The build runs from the project root even when only one package inside it
	// is wanted, so imports of the project's own packages and any assets
	// referenced from outside the command directory still resolve.
	root, pkg, err := splitPackage(opts.SourceDir)
	if err != nil {
		return nil, err
	}

	// FyneApp.toml usually sits at the root, but a project whose command lives
	// in a subdirectory may keep it there instead.
	metaDir := root
	if pkg != "." {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(pkg), MetadataName)); err == nil {
			metaDir = filepath.Join(root, filepath.FromSlash(pkg))
		}
	}
	app, err := LoadMetadata(metaDir)
	if err != nil {
		return nil, err
	}
	if len(opts.Arches) == 0 {
		return nil, fmt.Errorf("no architectures to build")
	}

	// Every architecture in a release shares one build number. fyne bumps the
	// number in FyneApp.toml on each package run, so the number this release
	// publishes is the one the first build would have produced, and it is
	// pinned for the rest of them.
	number := app.Details.Build + 1

	spec, err := resolve(app, opts, number)
	if err != nil {
		return nil, err
	}

	// The bumping happens inside the container against the mounted source, so
	// restore the number that was actually published however the build ends.
	defer func() {
		if err := setBuildNumber(metaDir, number); err != nil {
			logf("could not record build %d in %s: %v", number, MetadataName, err)
		}
	}()

	where := ""
	if pkg != "." {
		where = " from " + pkg
	}
	logf("cross-building %s %s (build %d)%s for %s",
		app.Details.Name, app.Details.Version, number, where, strings.Join(opts.Arches, ", "))
	if err := runFyneCross(root, pkg, app, opts, number); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return nil, err
	}

	work, err := os.MkdirTemp("", "fyshpkg-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)

	var results []*Result
	for _, arch := range opts.Arches {
		bundle := bundlePath(root, app.Details.Name, arch)
		if _, err := os.Stat(bundle); err != nil {
			return nil, fmt.Errorf("fyne-cross produced no %s bundle: %w", arch, err)
		}

		staging := filepath.Join(work, arch)
		installed, err := unpack(bundle, staging, spec.Prefix)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", arch, err)
		}

		archSpec := *spec
		archSpec.Arch = arch
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
	return results, nil
}

// splitPackage separates the project root from the package to build.
//
// A relative path below the working directory names a package inside the
// project here: "fyshpkg package ./cmd/fyshsaver" builds that command with the
// whole checkout around it. Anything else - ".", an absolute path, or a path
// starting ".." - names a project root in its own right.
func splitPackage(arg string) (root, pkg string, err error) {
	if arg == "" {
		arg = "."
	}
	if filepath.IsAbs(arg) {
		abs, err := filepath.Abs(arg)
		return abs, ".", err
	}

	clean := filepath.Clean(arg)
	if clean == "." || strings.HasPrefix(clean, "..") {
		abs, err := filepath.Abs(clean)
		return abs, ".", err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	if _, err := os.Stat(filepath.Join(cwd, clean)); err != nil {
		return "", "", err
	}
	return cwd, "./" + filepath.ToSlash(clean), nil
}

// bundlePath is where fyne-cross leaves the bundle for one architecture.
func bundlePath(source, appName, debArch string) string {
	return filepath.Join(source, "fyne-cross", "dist", "linux-"+crossArch[debArch], appName+".tar.xz")
}

// debFileName is the conventional archive file name: name, version and
// architecture, with any epoch dropped as Debian file names never carry one.
func debFileName(s *spec) string {
	version := s.Version
	if _, after, found := strings.Cut(version, ":"); found {
		version = after
	}
	return fmt.Sprintf("%s_%s_%s.deb", s.Name, version, s.Arch)
}

// spec is the fully resolved set of control fields for one package.
type spec struct {
	Name        string
	Version     string
	Arch        string
	Section     string
	Priority    string
	Maintainer  string
	Depends     []string
	Description string
	Homepage    string
	Prefix      string
}

func resolve(app *FyneApp, opts Options, number int) (*spec, error) {
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
		s.Name = DebianName(app.Details.Name)
	}
	if s.Version == "" {
		s.Version = DebianVersion(app.Details.Version, number)
	}
	if s.Description == "" {
		s.Description = app.Synopsis()
	}
	if s.Homepage == "" {
		s.Homepage = app.Website
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

	for _, arch := range opts.Arches {
		if _, ok := crossArch[arch]; !ok {
			return nil, fmt.Errorf("fyne-cross cannot build the %s architecture", arch)
		}
	}
	if !validName.MatchString(s.Name) {
		return nil, fmt.Errorf("%q is not a valid Debian package name — set one with -name", s.Name)
	}
	if !validVersion.MatchString(s.Version) {
		return nil, fmt.Errorf("%q is not a valid Debian version — set one with -version", s.Version)
	}
	if s.Maintainer == "" {
		return nil, fmt.Errorf("no maintainer set — add \"maintainer\" to the package section of repo.json, or pass -maintainer")
	}
	return s, nil
}

// runFyneCross builds every architecture in one invocation, from the project
// root, building only pkg when that is not the whole project.
//
// The application's identity is passed explicitly rather than left to
// fyne-cross, which reads FyneApp.toml from its own working directory: that is
// the root here, and the file may well sit beside the command instead. Without
// this, a subdirectory build would be named after the root and stamped version
// 1.0.0. The build number is pinned so every architecture shares one.
func runFyneCross(root, pkg string, app *FyneApp, opts Options, number int) error {
	if _, err := exec.LookPath("fyne-cross"); err != nil {
		return fmt.Errorf("fyne-cross is not installed: go install github.com/fyne-io/fyne-cross@latest")
	}

	arches := make([]string, 0, len(opts.Arches))
	for _, arch := range opts.Arches {
		arches = append(arches, crossArch[arch])
	}

	args := []string{
		"linux",
		"--arch", strings.Join(arches, ","),
		"--name", app.Details.Name,
		"--app-build", strconv.Itoa(number),
	}
	if app.Details.Version != "" {
		args = append(args, "--app-version", app.Details.Version)
	}
	if app.Details.ID != "" {
		args = append(args, "--app-id", app.Details.ID)
	}
	if icon := iconPath(root, app); icon != "" {
		args = append(args, "--icon", icon)
	}
	if opts.Release {
		args = append(args, "--release")
	}
	if opts.Tags != "" {
		args = append(args, "--tags", opts.Tags)
	}
	if pkg != "." {
		args = append(args, pkg)
	}

	cmd := exec.Command("fyne-cross", args...)
	cmd.Dir = root
	cmd.Stdout = os.Stderr // keep our stdout clean for the tool's own output
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("fyne-cross %s: %w (is a container engine running?)", strings.Join(args, " "), err)
	}
	return nil
}

// iconPath rewrites the icon named in FyneApp.toml to be relative to the
// project root, since that is where fyne-cross resolves it from. A command in
// a subdirectory typically points back up at a shared asset.
func iconPath(root string, app *FyneApp) string {
	if app.Details.Icon == "" {
		return ""
	}
	rel, err := filepath.Rel(root, filepath.Join(app.dir, app.Details.Icon))
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

// unpack extracts a fyne bundle into staging, dropping the bundle's top level
// directory and its Makefile, and moving /usr/local onto the prefix. It
// returns the installed paths, relative to staging.
func unpack(bundle, staging, prefix string) ([]string, error) {
	f, err := os.Open(bundle)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	xzr, err := xz.NewReader(f)
	if err != nil {
		return nil, err
	}

	var installed []string
	tr := tar.NewReader(xzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		rel := relocate(hdr.Name, prefix)
		if rel == "" {
			continue
		}
		dest := filepath.Join(staging, filepath.FromSlash(rel))
		if !strings.HasPrefix(dest, staging+string(os.PathSeparator)) {
			return nil, fmt.Errorf("bundle entry escapes the staging directory: %s", hdr.Name)
		}

		// Directory entries are skipped: every directory that holds a file is
		// created below, so honouring them would keep the now-empty usr/local
		// that relocating the payload leaves behind.
		switch hdr.Typeflag {
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return nil, err
			}
			mode := fs.FileMode(0o644)
			if hdr.Mode&0o111 != 0 {
				mode = 0o755
			}
			out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return nil, err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return nil, err
			}
			if err := out.Close(); err != nil {
				return nil, err
			}
			installed = append(installed, rel)
		}
	}

	if len(installed) == 0 {
		return nil, fmt.Errorf("the fyne bundle contained no files to install")
	}
	sort.Strings(installed)
	return installed, nil
}

// relocate maps a path inside a fyne bundle to its place in the package, or
// returns "" for entries that do not belong in a .deb.
//
// Bundle layouts differ: fyne wraps the payload in a directory named after the
// binary, fyne-cross writes usr/ at the top level. Anchoring on the usr
// segment handles both, and drops the bundle's Makefile — its manual
// installer, useless inside a package — along with anything else outside it.
func relocate(name, prefix string) string {
	parts := strings.Split(path.Clean(name), "/")
	for i, part := range parts {
		if part != "usr" {
			continue
		}
		rest := parts[i+1:]
		// fyne installs below /usr/local; Debian policy reserves that for the
		// local administrator, so packages must use the prefix instead.
		if len(rest) > 0 && rest[0] == "local" {
			rest = rest[1:]
		}
		if len(rest) == 0 {
			return "" // the directory entry itself
		}
		return path.Join(strings.TrimPrefix(prefix, "/"), path.Join(rest...))
	}
	return ""
}
