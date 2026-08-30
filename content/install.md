---
title: Installing the archive
lead: Add packages.fyshos.com to apt, verify it, and remove it again if you change your mind.
---

## 1. Install the signing key

The archive is signed, so apt needs the public key before it will trust an
update. Keep it out of the deprecated global keyring and drop it in
`/usr/share/keyrings` instead:

```sh
sudo install -m 0755 -d /usr/share/keyrings
sudo curl -fsSL https://packages.fyshos.com/fyshos-archive-keyring.gpg \
  -o /usr/share/keyrings/fyshos-archive-keyring.gpg
sudo chmod 0644 /usr/share/keyrings/fyshos-archive-keyring.gpg
```

If `curl` is not installed, use `wget -qO-` in its place.

## 2. Add the source

### Debian 12+, Ubuntu 24.04+ (deb822)

```sh
sudo tee /etc/apt/sources.list.d/fyshos.sources > /dev/null <<'SOURCES'
Types: deb
URIs: https://packages.fyshos.com
Suites: stable
Components: main
Signed-By: /usr/share/keyrings/fyshos-archive-keyring.gpg
SOURCES
```

### Older releases (one-line format)

```sh
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/fyshos-archive-keyring.gpg] https://packages.fyshos.com stable main" \
  | sudo tee /etc/apt/sources.list.d/fyshos.list > /dev/null
```

## 3. Update and install

```sh
sudo apt update
sudo apt install fysh-desktop
```

## Checking what you added

Confirm apt is reading the archive and which key it trusts:

```sh
apt policy
gpg --show-keys /usr/share/keyrings/fyshos-archive-keyring.gpg
```

`apt policy <package>` shows which version apt would install and which archive
it would come from.

## Removing the archive

```sh
sudo rm -f /etc/apt/sources.list.d/fyshos.sources \
           /etc/apt/sources.list.d/fyshos.list \
           /usr/share/keyrings/fyshos-archive-keyring.gpg
sudo apt update
```

Packages already installed stay installed. Remove them with `apt remove` first
if you want a clean system.

## Troubleshooting

**`NO_PUBKEY` or `signatures couldn't be verified`** — the keyring is missing or
in the wrong place. Repeat step 1 and check the path in your `.sources` file
matches where the key actually landed.

**`Release file is not valid yet` or `has expired`** — the archive's
`Valid-Until` window has passed, or your system clock is wrong. Check the date
with `timedatectl`; if the clock is right, the archive needs reindexing.

**`404 Not Found` on a `Packages` file** — apt is asking for an architecture the
archive does not publish. The archive carries `amd64` and `arm64`; pin one with
`arch=` in the source definition.

## Browsing by hand

The archive is a plain static tree, so you can read it in a browser or fetch a
package directly:

- [/dists/stable/Release](/dists/stable/Release) — the signed index
- [/dists/stable/main/binary-amd64/Packages](/dists/stable/main/binary-amd64/Packages) — the amd64 package list
- [/pool/main/](/pool/main/) — the `.deb` files themselves
