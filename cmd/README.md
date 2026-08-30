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

## Adding your own scripts

Anything else that helps manage the archive belongs here: build wrappers,
release scripts, mirrors. Go programs go in their own directory under `cmd/`
(they share `cmd/go.mod`); shell scripts can sit at the top level of `cmd/`.

The Go packages behind `fyshpkg`:

- `internal/deb` — reads `.deb` files: ar, the control tarball and its
  gzip/xz/zstd variants, and the control stanza.
- `internal/repo` — the archive: pool layout, index generation, signing,
  integrity checks and the website's data file.
- `internal/build` — `FyneApp.toml`, and the fyne-cross-to-dpkg build
  pipeline.
