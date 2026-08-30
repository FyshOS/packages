# cmd

Tooling for the archive. Nothing in this directory is published: `hugo.toml`
mounts only `content`, `layouts`, `assets`, `data`, `static` and `repo`, so
`cmd/` never reaches `public/`.

## fyshpkg

`fyshpkg` is the only tool you need for day-to-day archive work. Install it
once and it will find the project from any working directory by looking for
`repo.json`:

```sh
cd cmd && go install ./fyshpkg
```

Or run it without installing, from this directory:

```sh
go run ./fyshpkg <command>
```

### Commands

| Command | What it does |
| --- | --- |
| `fyshpkg add [-c main] <file.deb>...` | Copies packages into `repo/pool/` and reindexes |
| `fyshpkg rm [-v version] [-a arch] <name>` | Deletes matching packages from the pool and reindexes |
| `fyshpkg index` | Rebuilds `repo/dists/` from whatever is in the pool |
| `fyshpkg list [-json]` | Shows what the archive publishes |
| `fyshpkg check` | Verifies the published metadata against the pool |
| `fyshpkg key [-o static] [-n name]` | Exports the public signing key for the website |

`add` and `rm` reindex automatically, so `index` is only needed if you have
copied `.deb` files into the pool by hand or changed `repo.json`.

### What indexing writes

Every run of `fyshpkg index` deletes and rewrites `repo/dists/<suite>` from
scratch, so the metadata can never drift from the pool:

- `main/binary-<arch>/Packages` and `Packages.gz` — one stanza per package,
  with `Filename`, `Size` and checksums recomputed from the file on disk.
  Packages built `Architecture: all` are folded into every architecture so
  older apt clients, which do not fetch `binary-all`, still see them.
- `main/binary-<arch>/Release` — the small per-architecture marker file.
- `Release` — the suite index, listing an MD5 and SHA256 for every file above.
- `InRelease` and `Release.gpg` — signatures, when `signingKey` is set.

It also rewrites `data/packages.json`, which is what the website's package
table renders from. Only the newest version of each package and architecture
appears there; the pool keeps every version.

### Packages nobody should see again

`fyshpkg rm` deletes the `.deb` from the pool. Anyone who already installed it
keeps it — apt does not uninstall packages that vanish from an archive. If a
package was published in error, remove it, reindex, and bump the version of the
replacement so upgrades pull it in.

## Adding your own scripts

Anything else that helps manage the archive belongs here: build wrappers,
release scripts, mirrors. Go programs go in their own directory under `cmd/`
(they share `cmd/go.mod`); shell scripts can sit at the top level of `cmd/`.
