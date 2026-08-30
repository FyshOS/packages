---
title: FyshOS Packages
lead: The official Debian archive for FyshOS. Add it to apt and install FyshOS components the same way you install anything else.
---

## Quick start

On Debian 12 or newer and Ubuntu 24.04 or newer:

```sh
sudo install -m 0755 -d /usr/share/keyrings
sudo curl -fsSL https://packages.fyshos.com/fyshos-archive-keyring.gpg \
  -o /usr/share/keyrings/fyshos-archive-keyring.gpg

sudo tee /etc/apt/sources.list.d/fyshos.sources > /dev/null <<'SOURCES'
Types: deb
URIs: https://packages.fyshos.com
Suites: stable
Components: main
Signed-By: /usr/share/keyrings/fyshos-archive-keyring.gpg
SOURCES

sudo apt update
```

Then install what you need, for example:

```sh
sudo apt install fysh-desktop
```

[Full instructions, including older releases →](/install/)

## What is in here

The archive publishes the `stable` suite with a single `main` component, built
for `amd64` and `arm64`. Packages marked `Architecture: all` are available on
every architecture.

See the [package list](/packages/) for everything currently published.

## Verifying the archive

Every `Release` file is signed with the FyshOS archive key. `apt` checks that
signature on each update, so a tampered mirror or a corrupted download is
rejected before anything is installed. The public key is published at
[/fyshos-archive-keyring.asc](/fyshos-archive-keyring.asc) in armoured form and
at [/fyshos-archive-keyring.gpg](/fyshos-archive-keyring.gpg) for
`/usr/share/keyrings`.
