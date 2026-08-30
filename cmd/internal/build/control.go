package build

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// writeControl produces the DEBIAN directory: the control stanza and the
// md5sums manifest that dpkg uses to detect locally modified files.
func writeControl(staging string, s *spec, installed []string) error {
	debian := filepath.Join(staging, "DEBIAN")
	if err := os.MkdirAll(debian, 0o755); err != nil {
		return err
	}

	size, err := installedSize(staging, installed)
	if err != nil {
		return err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Package: %s\n", s.Name)
	fmt.Fprintf(&b, "Version: %s\n", s.Version)
	fmt.Fprintf(&b, "Architecture: %s\n", s.Arch)
	fmt.Fprintf(&b, "Maintainer: %s\n", s.Maintainer)
	fmt.Fprintf(&b, "Installed-Size: %d\n", size)
	if len(s.Depends) > 0 {
		fmt.Fprintf(&b, "Depends: %s\n", strings.Join(s.Depends, ", "))
	}
	fmt.Fprintf(&b, "Section: %s\n", s.Section)
	fmt.Fprintf(&b, "Priority: %s\n", s.Priority)
	if s.Homepage != "" {
		fmt.Fprintf(&b, "Homepage: %s\n", s.Homepage)
	}
	fmt.Fprintf(&b, "Description: %s\n", synopsisLine(s.Description))

	if err := os.WriteFile(filepath.Join(debian, "control"), []byte(b.String()), 0o644); err != nil {
		return err
	}
	return writeMD5Sums(staging, debian, installed)
}

// synopsisLine keeps the description to the single line Debian expects, since
// the fyne metadata has nowhere to put an extended description.
func synopsisLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(line)
}

// installedSize is the total size of the payload in kilobytes, the unit the
// Installed-Size control field uses.
func installedSize(staging string, installed []string) (int64, error) {
	var total int64
	for _, rel := range installed {
		info, err := os.Stat(filepath.Join(staging, filepath.FromSlash(rel)))
		if err != nil {
			return 0, err
		}
		total += (info.Size() + 1023) / 1024
	}
	return total, nil
}

func writeMD5Sums(staging, debian string, installed []string) error {
	var b bytes.Buffer
	for _, rel := range installed {
		raw, err := os.ReadFile(filepath.Join(staging, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		sum := md5.Sum(raw)
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), rel)
	}
	return os.WriteFile(filepath.Join(debian, "md5sums"), b.Bytes(), 0o644)
}

// runDpkgDeb wraps the staging tree into a .deb. --root-owner-group keeps the
// archive reproducible whichever user built it.
func runDpkgDeb(staging, out string) error {
	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		return fmt.Errorf("dpkg-deb is not installed: apt install dpkg")
	}

	cmd := exec.Command("dpkg-deb", "--build", "--root-owner-group", staging, out)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("dpkg-deb --build: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// hostArch asks dpkg which Debian architecture this machine is, since that is
// what a locally compiled binary will run on.
func hostArch() (string, error) {
	out, err := exec.Command("dpkg", "--print-architecture").Output()
	if err != nil {
		return "", fmt.Errorf("could not determine the host architecture, pass -arch: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
