package repo

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
		return fmt.Errorf("gpg %v: %w: %s", args, err, stderr.String())
	}
	return nil
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
