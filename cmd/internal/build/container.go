package build

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// buildScript runs inside the container. It installs what the build needs,
// runs the project's own make targets, and works out the runtime dependencies
// from the binaries that came out.
//
// The container runs as the target architecture, so "make" behaves exactly as
// it does on that machine and there is no cross-compilation to configure.
const buildScript = `
set -e
export DEBIAN_FRONTEND=noninteractive

if [ -n "$BACKPORTS" ]; then
	# The base is too old for a usable Go, so take just that from backports.
	echo "deb http://deb.debian.org/debian $BACKPORTS main" > /etc/apt/sources.list.d/backports.list
fi

apt-get update -qq
# build-essential rather than a shorter list: without a C compiler Go quietly
# turns cgo off, and a Fyne build then fails deep in go-gl with "build
# constraints exclude all Go files" rather than saying what is missing.
apt-get install -y -qq --no-install-recommends build-essential pkg-config ca-certificates $BUILD_DEPS
if [ -n "$BACKPORTS" ]; then
	apt-get install -y -qq --no-install-recommends -t "$BACKPORTS" golang-go
else
	apt-get install -y -qq --no-install-recommends golang-go
fi

go version

# Explicit, so a missing compiler fails loudly here rather than silently
# producing a build with cgo disabled.
export CGO_ENABLED=1

cd /src
make $MAKE_TARGETS
make $INSTALL_TARGET DESTDIR=/out PREFIX="$PREFIX"

# Work out the runtime dependencies from the binaries themselves, rather than
# keeping a hand-written list in step with the code. dpkg-shlibdeps wants a
# source package around it, so give it the smallest one that will do.
mkdir -p /src/debian
printf 'Source: %s\n\nPackage: %s\nArchitecture: any\n' "$PKG_NAME" "$PKG_NAME" > /src/debian/control

ELVES=""
for FILE in $(find /out -type f); do
	# Only ELF objects, identified by their magic rather than by permissions,
	# since an install target may leave a stripped bit or two behind.
	case "$(od -An -tx1 -N4 "$FILE" | tr -d ' \n')" in
		7f454c46) ELVES="$ELVES $FILE" ;;
	esac
done

if [ -n "$ELVES" ]; then
	dpkg-shlibdeps -O --ignore-missing-info $ELVES 2>/dev/null \
		| sed -n 's/^shlibs:Depends=//p' > /meta/depends || true
fi

# A rootful container writes as the real root, so hand everything back to the
# user who asked for the build. Rootless runs leave CHOWN_TO empty: there the
# container's root is already the invoking user on the outside.
if [ -n "$CHOWN_TO" ]; then
	chown -R "$CHOWN_TO" /out /meta
	chown -R "$CHOWN_TO" /root/.cache/go-build /root/go/pkg/mod 2>/dev/null || true
fi
`

// runContainer performs one architecture's build.
func runContainer(source, staging, meta, arch string, img buildImage, s *spec, opts ProjectOptions, rootful bool) error {
	// The build writes into the source tree, so work from a copy: a foreign
	// architecture's objects have no business in the developer's checkout.
	work := filepath.Dir(staging)
	src := filepath.Join(work, "src")
	for _, dir := range []string{staging, meta, src} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := copyTree(source, src); err != nil {
		return err
	}

	// A per-architecture module and build cache, so repeat builds are not
	// paying for a full recompile every time.
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	goCache := filepath.Join(cache, "fyshpkg", arch, "go-build")
	goMod := filepath.Join(cache, "fyshpkg", arch, "go-mod")
	for _, dir := range []string{goCache, goMod} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	image := img.image
	if opts.Image != "" {
		image = opts.Image
	}

	makeTargets := opts.MakeTargets
	if len(makeTargets) == 0 {
		makeTargets = []string{"build"}
	}
	installTarget := opts.InstallTarget
	if installTarget == "" {
		installTarget = "install"
	}

	// Only the podman command is elevated, and only for an architecture that
	// cannot run any other way: rootful podman does not put the container in a
	// user namespace, which is what lets it see the host's binfmt handlers.
	podman := "podman"
	var sudoArgs []string
	var netArgs []string
	chownTo := ""
	if rootful {
		podman = "sudo"
		sudoArgs = []string{"podman"}
		chownTo = fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())

		// Rootful containers get their own network and a copy of the host's
		// /etc/resolv.conf, which is useless when that points at a local stub
		// resolver such as systemd-resolved on 127.0.0.53. Sharing the host
		// network makes name resolution behave as it does outside. Rootless
		// runs need none of this: pasta hands them the real upstream servers.
		netArgs = []string{"--network=host"}
	}

	args := append(sudoArgs, "run", "--rm", "--arch="+img.podmanArch)
	args = append(args, netArgs...)
	args = append(args,
		"-v", src+":/src",
		"-v", staging+":/out",
		"-v", meta+":/meta",
		"-v", goCache+":/root/.cache/go-build",
		"-v", goMod+":/root/go/pkg/mod",
		"-e", "BACKPORTS="+img.backports,
		"-e", "BUILD_DEPS="+strings.Join(opts.BuildDepends, " "),
		"-e", "MAKE_TARGETS="+strings.Join(makeTargets, " "),
		"-e", "INSTALL_TARGET="+installTarget,
		"-e", "PREFIX="+s.Prefix,
		"-e", "PKG_NAME="+s.Name,
		"-e", "CHOWN_TO="+chownTo,
		image, "sh", "-c", buildScript,
	)

	cmd := exec.Command(podman, args...)
	cmd.Stdout = os.Stderr // keep our stdout clean for the tool's own output
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s run: %w", podman, err)
	}
	return nil
}

// copyTree copies a source checkout, leaving behind the repository metadata
// and anything the host has already built there.
func copyTree(from, to string) error {
	entries, err := os.ReadDir(from)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		switch entry.Name() {
		case ".git", "debian":
			continue
		}
		cmd := exec.Command("cp", "-a", filepath.Join(from, entry.Name()), to+string(os.PathSeparator))
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("copying %s: %w: %s", entry.Name(), err, stderr.String())
		}
	}
	return nil
}
