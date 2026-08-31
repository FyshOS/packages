# cmd

Tooling for the archive. Nothing in this directory is published: `hugo.toml`
mounts only `content`, `layouts`, `assets`, `data`, `static` and `repo`, so
`cmd/` never reaches `public/`.

## fyshpkg

`fyshpkg` is the only tool you need for day-to-day archive work. Install it
once:

```sh
cd cmd && go install ./fyshpkg
```

Or run it without installing, from this directory:

```sh
go run ./fyshpkg <command>
```

It locates the archive in one of three ways, in order: the `-repo` flag (on
`package`), the `FYSHPKG_REPO` environment variable, then `repo.json` found by
walking up from the working directory. The last of those covers everything run
from inside the archive; set `FYSHPKG_REPO` for everything else.

### Commands

| Command | What it does |
| --- | --- |
| `fyshpkg package [flags] [dir]` | Cross-builds a Fyne application into one `.deb` per architecture and adds them |
| `fyshpkg make [flags] [dir]` | Builds a makefile project the same way, using its own install target |
| `fyshpkg add [-c main] <file.deb>...` | Copies packages into `repo/pool/` and reindexes |
| `fyshpkg rm [-v version] [-a arch] <name>` | Deletes matching packages from the pool and reindexes |
| `fyshpkg index` | Rebuilds `repo/dists/` from whatever is in the pool |
| `fyshpkg list [-json]` | Shows what the archive publishes |
| `fyshpkg check` | Verifies the published metadata against the pool |
| `fyshpkg key [-o static] [-n name]` | Exports the public signing key for the website |

`package`, `add` and `rm` reindex automatically, so `index` is only needed if
you have copied `.deb` files into the pool by hand or changed `repo.json`.

## Building from source

`fyshpkg package` is the command a FyshOS developer reaches for. Point it at a
Fyne application and it cross-builds every architecture the archive carries,
then publishes them all.

Applications live in their own repositories, outside the archive, so tell
fyshpkg where the archive is. Export it once in your shell profile:

```sh
export FYSHPKG_REPO=~/Code/FyshOS/packages.fyshos.com
```

and every command works from wherever you happen to be:

```sh
cd ~/Code/FyshOS/notes
fyshpkg package
```

Without the variable, `fyshpkg` looks for `repo.json` by walking up from the
working directory — which finds it anywhere inside the archive, and nowhere
inside an application. Per-invocation, `-repo` does the same job:

```sh
fyshpkg package -repo ~/Code/FyshOS/packages.fyshos.com ~/Code/FyshOS/notes
```

```
cross-building Fysh Notes 0.4.2 (build 21) for amd64, arm64, i386
packaged fysh-notes 0.4.2-21 (amd64) from 3 file(s)
packaged fysh-notes 0.4.2-21 (arm64) from 3 file(s)
packaged fysh-notes 0.4.2-21 (i386) from 3 file(s)
added fysh-notes 0.4.2-21 (amd64) -> pool/main/f/fysh-notes/fysh-notes_0.4.2-21_amd64.deb
added fysh-notes 0.4.2-21 (arm64) -> pool/main/f/fysh-notes/fysh-notes_0.4.2-21_arm64.deb
added fysh-notes 0.4.2-21 (i386) -> pool/main/f/fysh-notes/fysh-notes_0.4.2-21_i386.deb
indexed 3 package(s) across stable, signed with …
```

It leans on the tools that already know how to do each job:

1. **`fyne-cross linux --arch amd64,arm64,386`** compiles each architecture in its
   own container and lays out the binary, `.desktop` entry and icon. Use
   `-release` for a release build and `-tags` for build tags.
2. Each bundle is restaged under `/usr`. Fyne installs below `/usr/local`,
   which Debian policy reserves for the local administrator, so a package may
   not use it. Override the destination with `-prefix` if you need to.
3. A `DEBIAN/control` stanza is written per architecture from `FyneApp.toml`
   and `repo.json`, along with an `md5sums` manifest and a computed
   `Installed-Size`.
4. **`dpkg-deb --build --root-owner-group`** wraps each one up, and they all go
   into the pool before a single reindex.

The architectures come from `architectures` in `repo.json`, so what the archive
publishes and what gets built cannot drift apart. Build a subset with
`-arch amd64`. Pass `-o <dir>` to keep the `.deb` files as well, or `-no-add`
to build without publishing.

### The shared build number

`fyne package` increments `Build` in `FyneApp.toml` on every run — including
when it is handed an explicit build number, and once per architecture when
fyne-cross drives it. Left alone, the two binaries in one release would carry
different build numbers and land in the archive as different versions.

So `fyshpkg` takes the number the first build would have produced, pins it for
every architecture with `--app-build`, and writes it back to `FyneApp.toml`
afterwards. One release, one build number, one Debian revision across all
architectures, and the file ends up holding exactly what shipped so the next
release carries on from there. The rewrite touches only the `Build` line, and
happens even if a build fails partway.

### Where each control field comes from

| Field | Source |
| --- | --- |
| `Package` | `[Details] Name`, lower-cased and hyphenated — `-name` to override |
| `Version` | `[Details] Version` and the shared build number, as `version-build` — `-version` to override |
| `Architecture` | one package per entry in `architectures` — `-arch` to narrow |
| `Description` | `[LinuxAndBSD] Comment`, then `GenericName`, then the app name |
| `Homepage` | `Website` |
| `Maintainer`, `Section`, `Priority`, `Depends` | the `package` section of `repo.json` |

`FyneApp.toml` has nowhere to record a maintainer or dependencies, so those
come from `repo.json`. Packaging fails if no maintainer is set — Debian
requires one. The default `depends` list covers what a Fyne application links
against on a desktop system; narrow or widen it per package with `-depends`.

Note that fyne-cross reads `FyneApp.toml` from its working directory rather
than from its `-dir` flag, so `fyshpkg` runs it inside the source directory.
Name, id, icon and version are picked up from the file that way; only the build
number is forced.

### Requirements

`package` needs `fyne-cross`, `dpkg-deb`, and a container engine for
fyne-cross to build in:

```sh
go install github.com/fyne-io/fyne-cross@latest
sudo apt install dpkg podman
```

The first build of each architecture pulls a container image, so it takes a
while; later builds reuse both the image and the Go build cache in
`~/.cache/fyne-cross`. fyne-cross leaves its work in a `fyne-cross/` directory
inside the application source — worth adding to that project's `.gitignore`.

Everything else — parsing the `.deb`, indexing, signing — is handled by
`fyshpkg` itself.

## Building a makefile project

`fyshpkg package` suits a single Fyne application: one binary, one desktop
entry, one icon, all described by `FyneApp.toml`. Tyde and Fin are not that
shape. Tyde builds three binaries and ships a session file, two launcher
entries and an icon; Fin ships a systemd unit beside its binary. Both already
describe exactly that in their makefiles, so `fyshpkg make` uses the makefile
as the manifest instead of inventing a second one:

```sh
cd ~/Code/FyshOS/tyde
make repo
```

Whatever the install target stages under `DESTDIR` becomes the package
payload. Add a file to `make install` and it is in the next `.deb`, with
nothing to keep in step here.

### How a build runs

Each architecture is built inside a container **of that architecture**, using
podman's `--arch`, so nothing is cross-compiled: cgo, `pkg-config` and the
system headers all behave as they would on that machine. That matters for
Tyde and Fin, which link against X11, wayland and PAM.

The base image matches what the ISO is built from — `debian:trixie` for amd64
and arm64, `debian:bookworm` for i386, since Trixie dropped i386. Bookworm's Go
is too old for current Fyne, so `golang-go` comes from `bookworm-backports`
there; the 64bit images use the packaged Go. Module and build caches are kept
per architecture under `~/.cache/fyshpkg`, so a second build is much quicker
than the first.

The build works on a copy of the checkout, never the checkout itself — a
foreign architecture's object files have no business in your working tree.

### Dependencies and versions

Runtime dependencies are read off the built binaries with `dpkg-shlibdeps`
inside the container, so they are accurate and versioned (`libc6 (>= 2.38)`
rather than a hopeful `libc6`) and there is no hand-written list to drift.
`-depends` is only a fallback for when that finds nothing.

`-build-deps` is the one list the makefile does have to carry: the packages
the build itself needs. It sits next to the target it belongs to.

A subdirectory argument names where the command lives, exactly as it does for
`fyshpkg package`. Saver keeps its `FyneApp.toml` beside its command, so:

```sh
fyshpkg make cmd/fyshsaver
```

builds the whole project with its makefile as usual, and reads the version and
build number from `cmd/fyshsaver/FyneApp.toml`.

### Which command to use

`fyshpkg package` is the shorter route for a self-contained Fyne application,
but it builds inside the fyne-cross image, which is fixed. A project needing a
library that image does not carry has to use `fyshpkg make` instead, where
`-build-deps` can install it: Saver links against PAM, whose headers fyne-cross
does not ship, so it is packaged from its own makefile alongside Tyde and Fin.

Versions come from the same place a single Fyne application's do, so
everything in the archive reads alike:

| Where the version comes from | Example |
| --- | --- |
| `FyneApp.toml`, as `Version` and `Build` | `0.1.0-5` |
| `git describe`, when there is no `FyneApp.toml` | `0.4.0+393.g5f1194d5-1` |
| `-version`, which overrides both | whatever you pass |

With a `FyneApp.toml` the build number is incremented and written back, once
every architecture has been packaged — a release that fell over halfway does
not consume one. `Description` and `Homepage` fall back to its `Comment` and
`Website` too, so a project that has one need pass fewer flags.

Without one the version is the repository's position in history: `0.4.0-1` on
tag `v0.4.0`, and `0.4.0+393.g5f1194d5-1` past it. The `+` form sorts after the
tagged release and before the next one, so a development build never masks a
later tag. Add a `FyneApp.toml` to move a project onto the first scheme.

### Requirements

`make` needs podman and `dpkg-deb`, and the project needs a makefile whose
install target honours `DESTDIR` and `PREFIX`:

```sh
sudo apt install podman dpkg
```

## Adding your own scripts

Anything else that helps manage the archive belongs here: build wrappers,
release scripts, mirrors. Go programs go in their own directory under `cmd/`
(they share `cmd/go.mod`); shell scripts can sit at the top level of `cmd/`.

The Go packages behind `fyshpkg`:

- `internal/deb` — reads `.deb` files: ar, the control tarball and its
  gzip/xz/zstd variants, and the control stanza.
- `internal/repo` — the archive: pool layout, index generation, signing,
  integrity checks and the website's data file.
- `internal/build` — `FyneApp.toml`, the fyne-cross-to-dpkg pipeline for
  single applications, and the container-and-makefile pipeline for projects
  that install themselves.
