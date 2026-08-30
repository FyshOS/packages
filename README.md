# packages.fyshos.com

The FyshOS Debian archive and the website that documents it, in one repository.
Both are static: Hugo renders the site, `fyshpkg` renders the archive, and
GitHub Pages serves the result at <https://packages.fyshos.com>.

```
.
├── content/            website pages (Markdown)
├── layouts/            Hugo templates
├── assets/             stylesheet, processed by Hugo
├── static/             copied to the site root verbatim (favicon, keyring, JS)
├── data/packages.json  generated — the package table on the website
├── repo/               the Debian archive, served from the site root
│   ├── dists/          generated indexes and signatures
│   └── pool/           the .deb files
├── cmd/                tooling; never published — see cmd/README.md
├── repo.json           archive configuration
└── hugo.toml           site configuration
```

`hugo.toml` mounts `repo/` into the site root, so a single `hugo` run produces
`public/index.html` alongside `public/dists/…` and `public/pool/…`. The archive
paths apt asks for are exactly the paths GitHub Pages serves.

## Publishing a package

Install the tool once, and tell it where the archive lives:

```sh
cd cmd && go install ./fyshpkg
echo 'set -gx FYSHPKG_REPO ~/Code/FyshOS/packages.fyshos.com' >> ~/.config/fish/config.fish
```

(`export FYSHPKG_REPO=...` in `~/.bashrc` or `~/.zshrc` for bash or zsh.)
Applications live in their own repositories, so without that variable fyshpkg
has no way to find the archive from an application's source tree. `-repo` sets
it per invocation instead.

From a Fyne application's source directory, one command builds and publishes
it for every architecture the archive carries — `fyne-cross` compiles each one
in a container and lays out the desktop entry and icon, `dpkg-deb` wraps them
as policy-shaped `.deb` files, and they land in the pool:

```sh
cd ~/Code/FyshOS/notes
fyshpkg package
```

```
cross-building Fysh Notes 0.4.2 (build 21) for amd64, arm64, i386
packaged fysh-notes 0.4.2-21 (amd64) from 3 file(s)
packaged fysh-notes 0.4.2-21 (arm64) from 3 file(s)
packaged fysh-notes 0.4.2-21 (i386) from 3 file(s)
```

All architectures in a release share one build number, so they land in the
archive as one version. It needs `fyne-cross` and a container engine —
see [cmd/README.md](cmd/README.md).

Or publish a `.deb` you already have:

```sh
fyshpkg add ~/build/fysh-desktop_1.2.3-1_amd64.deb
```

Either way the pool gains the package, every index is regenerated, the
`Release` file is re-signed and the website's package list is updated. Commit
and push to publish:

```sh
git add repo data && git commit -m "Publish fysh-desktop 1.2.3-1" && git push
```

Pushing to `main` runs the deploy workflow. See [cmd/README.md](cmd/README.md)
for the full command set and the packaging flags.

## Working on the website

```sh
make serve     # http://localhost:1313, with the archive mounted at /dists and /pool
make build     # a full production build into public/
```

Content lives in `content/`; the package table is the `{{</* packages */>}}`
shortcode, which reads `data/packages.json`.

## Packaging defaults

`FyneApp.toml` describes an application, not a Debian package, so the fields it
has nowhere to record live in the `package` section of `repo.json`:

```json
"package": {
  "maintainer": "FyshOS <andy@andy.xyz>",
  "section": "misc",
  "priority": "optional",
  "depends": ["libc6", "libgl1", "libx11-6", "…"]
}
```

`maintainer` is required — Debian will not accept a package without one. The
`depends` list is the runtime library set a Fyne application needs on a desktop
system, and applies to everything built with `fyshpkg package`; override it for
a single package with `-depends`.

The `architectures` list drives both what the archive advertises and what
`fyshpkg package` cross-builds, so the two cannot drift apart.

## Signing

Set `signingKey` in `repo.json` to the key id or user id of the archive key,
then export the public half for the website:

```sh
fyshpkg key            # writes static/fyshos-archive-keyring.{asc,gpg}
fyshpkg index          # re-signs Release
```

Signing runs on your machine, using your GnuPG keyring — the private key is
never needed in CI, and must never be committed. `FYSHPKG_SIGNING_KEY`
overrides `repo.json` if you need to sign with a different key temporarily.

Until a key is configured the archive is unsigned, and users have to opt in
with `[trusted=yes]` in their apt source. Set one up before announcing the
archive:

```sh
gpg --quick-generate-key "FyshOS Archive Signing Key <andy@andy.xyz>" rsa4096 sign never
gpg --list-secret-keys --keyid-format=long
```

## Archive layout

One suite (`stable`) and one component (`main`), built for `amd64`, `arm64`
and `i386` — `fyshpkg package` produces a `.deb` for each.
All suites listed in `repo.json` index the whole pool, which is what you want
for a single-suite archive; splitting packages across suites would need a
membership file that `fyshpkg` does not yet keep.

Change any of that in `repo.json` and run `fyshpkg index`.
