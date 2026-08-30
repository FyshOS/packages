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

```sh
cd cmd && go install ./fyshpkg     # once
fyshpkg add ~/build/fysh-desktop_1.2.3-1_amd64.deb
git add repo data && git commit -m "Publish fysh-desktop 1.2.3-1" && git push
```

`add` copies the package into the pool, regenerates every index, signs the
`Release` file and updates the website's package list. Pushing to `main` runs
the deploy workflow. See [cmd/README.md](cmd/README.md) for the full command
set.

## Working on the website

```sh
make serve     # http://localhost:1313, with the archive mounted at /dists and /pool
make build     # a full production build into public/
```

Content lives in `content/`; the package table is the `{{</* packages */>}}`
shortcode, which reads `data/packages.json`.

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

## Deployment

`.github/workflows/deploy.yml` runs on every push to `main`: it runs the Go
tests, runs `fyshpkg check` to confirm the committed metadata still matches the
pool, builds the site with Hugo, and deploys to GitHub Pages.

Enable **Settings → Pages → Source: GitHub Actions** on the repository, and set
the custom domain to `packages.fyshos.com`. `static/CNAME` keeps that setting
in the build output.

### Limits worth knowing

GitHub Pages caps a published site at **1 GB** with a **100 MB** limit per
file, and asks that sites stay under 100 GB of bandwidth a month. That is
comfortable for a desktop distribution's own packages, but the pool grows with
every release you keep. Prune old versions with `fyshpkg rm -v <version>` when
they stop being useful. Do not put `.deb` files in Git LFS — Pages serves LFS
pointers, not the file.

## Archive layout

One suite (`stable`) and one component (`main`), built for `amd64` and `arm64`.
All suites listed in `repo.json` index the whole pool, which is what you want
for a single-suite archive; splitting packages across suites would need a
membership file that `fyshpkg` does not yet keep.

Change any of that in `repo.json` and run `fyshpkg index`.
