package repo

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// sign produces InRelease (inline signature) and Release.gpg (detached) beside
// the Release file in dist. With no signing key configured both are removed,
// leaving an unsigned archive that apt will only accept with [trusted=yes].
func (c *Config) sign(dist string) error {
	release := filepath.Join(dist, "Release")
	inRelease := filepath.Join(dist, "InRelease")
	detached := filepath.Join(dist, "Release.gpg")

	if c.SigningKey == "" {
		os.Remove(inRelease)
		os.Remove(detached)
		return nil
	}

	if err := c.gpg("--clearsign", "--output", inRelease, release); err != nil {
		return err
	}
	return c.gpg("--armor", "--detach-sign", "--output", detached, release)
}

func (c *Config) gpg(args ...string) error {
	full := append([]string{"--batch", "--yes", "--local-user", c.SigningKey}, args...)
	cmd := exec.Command("gpg", full...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gpg %v: %w: %s%s", args, err,
			strings.TrimSpace(stderr.String()), passphraseHint(stderr.String()))
	}
	return nil
}

// passphraseHint recognises gpg failing because it could not ask for the key's
// passphrase, which is the usual reason signing breaks: there is no terminal
// for pinentry to prompt on, or GPG_TTY is not set.
func passphraseHint(stderr string) string {
	for _, sign := range []string{"Inappropriate ioctl", "No pinentry", "no pinentry", "Operation cancelled"} {
		if strings.Contains(stderr, sign) {
			return "\n  gpg could not ask for the signing key's passphrase." +
				"\n  Run this from a terminal, and make sure GPG_TTY is set:" +
				"\n    export GPG_TTY=$(tty)          # bash, zsh" +
				"\n    set -gx GPG_TTY (tty)          # fish" +
				"\n  gpg-agent then caches the passphrase for later runs."
		}
	}
	return ""
}

// ExportKey writes the archive's public signing key into the given directory,
// both armoured (.asc) and binary (.gpg, for /usr/share/keyrings).
func (c *Config) ExportKey(dir, base string) ([]string, error) {
	if c.SigningKey == "" {
		return nil, fmt.Errorf("no signingKey set in %s", ConfigName)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	var written []string
	for _, form := range []struct{ ext, flag string }{{".asc", "--armor"}, {".gpg", ""}} {
		args := []string{"--batch", "--yes"}
		if form.flag != "" {
			args = append(args, form.flag)
		}
		name := filepath.Join(dir, base+form.ext)
		args = append(args, "--output", name, "--export", c.SigningKey)

		cmd := exec.Command("gpg", args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return written, fmt.Errorf("gpg --export: %w: %s", err, stderr.String())
		}
		written = append(written, name)
	}
	return written, nil
}
