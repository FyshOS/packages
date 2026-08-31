// Package build turns a Fyne application's source directory into a .deb, using
// the fyne tool to lay the application out and dpkg-deb to wrap it.
package build

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// MetadataName is the file the fyne tool reads an application's identity from.
const MetadataName = "FyneApp.toml"

// FyneApp mirrors the parts of FyneApp.toml that matter when building a
// Debian package. Unknown keys in the file are ignored.
type FyneApp struct {
	Website     string
	Details     AppDetails
	LinuxAndBSD *LinuxAndBSD

	// dir is where the file was read from, so paths inside it that are
	// relative to it - the icon - can be resolved later.
	dir string
}

// AppDetails is the [Details] table: the application's identity and version.
type AppDetails struct {
	Icon    string
	Name    string
	ID      string
	Version string
	Build   int
}

// LinuxAndBSD is the [LinuxAndBSD] table, which carries the desktop-entry
// details that map neatly onto Debian control fields.
type LinuxAndBSD struct {
	GenericName string
	Categories  []string
	Comment     string
	Keywords    []string
	ExecParams  string
}

// LoadMetadata reads FyneApp.toml from an application's source directory.
func LoadMetadata(dir string) (*FyneApp, error) {
	name := filepath.Join(dir, MetadataName)
	raw, err := os.ReadFile(name)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no %s in %s — is this a Fyne application?", MetadataName, dir)
		}
		return nil, err
	}

	app := &FyneApp{dir: dir}
	if err := toml.Unmarshal(raw, app); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if app.Details.Name == "" {
		return nil, fmt.Errorf("%s: [Details] Name is not set", name)
	}
	return app, nil
}

// DebianName converts an application name into a valid Debian package name:
// lower case, with runs of anything unusable collapsed into a single dash.
func DebianName(appName string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(appName) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '+', r == '.':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.Trim(b.String(), "-.")
}

// DebianVersion combines the Fyne version and build number into a Debian
// version, treating the build number as the package revision.
func DebianVersion(version string, build int) string {
	if version == "" {
		version = "0.0.0"
	}
	if build < 1 {
		build = 1
	}
	return fmt.Sprintf("%s-%d", version, build)
}

var (
	validName    = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]+$`)
	validVersion = regexp.MustCompile(`^(\d+:)?[0-9][A-Za-z0-9.+~:-]*$`)
)

// Synopsis is the one-line description Debian shows in package listings.
func (a *FyneApp) Synopsis() string {
	if a.LinuxAndBSD != nil {
		if a.LinuxAndBSD.Comment != "" {
			return a.LinuxAndBSD.Comment
		}
		if a.LinuxAndBSD.GenericName != "" {
			return a.LinuxAndBSD.GenericName
		}
	}
	return a.Details.Name
}

var buildLine = regexp.MustCompile(`(?m)^([ \t]*)Build([ \t]*)=[ \t]*\d+[ \t]*$`)

// setBuildNumber records n as the build number in FyneApp.toml.
//
// fyne increments the number on every package run, so a release built for
// several architectures would otherwise leave the file several ahead of the
// number that was actually published. Writing it back keeps the file in step
// with the archive, and the next release carries on from there.
func setBuildNumber(dir string, n int) error {
	name := filepath.Join(dir, MetadataName)
	raw, err := os.ReadFile(name)
	if err != nil {
		return err
	}

	replacement := fmt.Appendf(nil, "${1}Build${2}= %d", n)
	if updated := buildLine.ReplaceAll(raw, replacement); !bytes.Equal(updated, raw) {
		return os.WriteFile(name, updated, 0o644)
	}
	if buildLine.Match(raw) {
		return nil // already correct
	}

	// No build number yet, so start one directly below [Details].
	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "[Details]" {
			continue
		}
		lines = append(lines[:i+1], append([]string{fmt.Sprintf("  Build = %d", n)}, lines[i+1:]...)...)
		return os.WriteFile(name, []byte(strings.Join(lines, "\n")), 0o644)
	}
	return fmt.Errorf("%s has no [Details] section", name)
}
