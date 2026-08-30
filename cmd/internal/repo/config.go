// Package repo maintains the Debian archive that lives under repo/ and the
// data file the Hugo site renders its package table from.
package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ConfigName is the marker file that identifies the project root.
const ConfigName = "repo.json"

// Config describes the archive. It is read from repo.json at the project root.
type Config struct {
	// Root is the archive directory, relative to the project root. Hugo mounts
	// it into the site root so that dists/ and pool/ are served from /.
	Root string `json:"root"`
	// DataDir holds the JSON the website reads its package list from.
	DataDir string `json:"dataDir"`

	Origin      string `json:"origin"`
	Label       string `json:"label"`
	Description string `json:"description"`

	Suites        []string `json:"suites"`
	Components    []string `json:"components"`
	Architectures []string `json:"architectures"`

	// SigningKey is the GnuPG key id or user id used to sign Release. When it
	// is empty the archive is left unsigned and apt needs [trusted=yes].
	// Set FYSHPKG_SIGNING_KEY to override it without editing the file.
	SigningKey string `json:"signingKey"`
	// ValidDays sets Valid-Until on Release. Zero omits the field.
	ValidDays int `json:"validDays"`

	// Package holds the control-field defaults `fyshpkg package` applies when
	// building a .deb from a Fyne application's source.
	Package PackageDefaults `json:"package"`

	dir string // absolute project root, not serialised
}

// PackageDefaults are the control fields that FyneApp.toml has nowhere to
// record, so the archive decides them.
type PackageDefaults struct {
	// Maintainer is the RFC822 name and address of whoever is answerable for
	// the packaging. Debian requires it, so packaging fails without one.
	Maintainer string `json:"maintainer"`
	Section    string `json:"section"`
	Priority   string `json:"priority"`
	// Depends is the runtime dependency list applied to every package built
	// from source. The default covers what a Fyne application needs on a
	// desktop system; override it per package with -depends.
	Depends []string `json:"depends"`
}

// EnvRoot names the environment variable that points at the archive, for
// working outside the project — building an application from its own source
// tree, most of all.
const EnvRoot = "FYSHPKG_REPO"

// Load finds the archive. FYSHPKG_REPO wins when it is set; otherwise
// repo.json is found by walking up from the working directory, so fyshpkg
// works from anywhere inside the project itself.
func Load() (*Config, error) {
	if dir := os.Getenv(EnvRoot); dir != "" {
		return LoadFrom(dir)
	}

	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	for {
		name := filepath.Join(dir, ConfigName)
		if _, err := os.Stat(name); err == nil {
			return load(name, dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, fmt.Errorf("no %s in this directory or any parent —\n"+
				"  run fyshpkg from the archive, pass -repo, or set %s to the archive directory",
				ConfigName, EnvRoot)
		}
		dir = parent
	}
}

// LoadFrom reads the archive rooted at dir, which may also be the path of the
// repo.json file itself.
func LoadFrom(dir string) (*Config, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(abs); err == nil && !info.IsDir() {
		abs = filepath.Dir(abs)
	}

	name := filepath.Join(abs, ConfigName)
	if _, err := os.Stat(name); err != nil {
		return nil, fmt.Errorf("no %s in %s", ConfigName, abs)
	}
	return load(name, abs)
}

func load(name, dir string) (*Config, error) {
	raw, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	c := &Config{dir: dir}
	if err := json.Unmarshal(raw, c); err != nil {
		return nil, fmt.Errorf("%s: %w", ConfigName, err)
	}

	if c.Root == "" {
		c.Root = "repo"
	}
	if c.DataDir == "" {
		c.DataDir = "data"
	}
	if len(c.Suites) == 0 {
		c.Suites = []string{"stable"}
	}
	if len(c.Components) == 0 {
		c.Components = []string{"main"}
	}
	if len(c.Architectures) == 0 {
		c.Architectures = []string{"amd64", "arm64"}
	}
	if c.Package.Section == "" {
		c.Package.Section = "misc"
	}
	if c.Package.Priority == "" {
		c.Package.Priority = "optional"
	}
	if len(c.Package.Depends) == 0 {
		c.Package.Depends = DefaultDepends
	}
	if key := os.Getenv("FYSHPKG_SIGNING_KEY"); key != "" {
		c.SigningKey = key
	}
	return c, nil
}

// DefaultDepends is the runtime library set a Fyne application links against
// on a Linux desktop. It is used when repo.json names no dependencies.
var DefaultDepends = []string{
	"libc6",
	"libgl1",
	"libx11-6",
	"libxcursor1",
	"libxi6",
	"libxinerama1",
	"libxrandr2",
	"libxxf86vm1",
}

// Dir is the absolute project root.
func (c *Config) Dir() string { return c.dir }

// Path joins elements onto the archive root.
func (c *Config) Path(elem ...string) string {
	return filepath.Join(append([]string{c.dir, c.Root}, elem...)...)
}

// HasComponent reports whether name is a configured component.
func (c *Config) HasComponent(name string) bool {
	for _, comp := range c.Components {
		if comp == name {
			return true
		}
	}
	return false
}

// BinaryArchitectures is the architecture list minus "all", which is folded
// into every real architecture rather than published on its own.
func (c *Config) BinaryArchitectures() []string {
	out := make([]string, 0, len(c.Architectures))
	for _, arch := range c.Architectures {
		if arch != "all" && arch != "source" {
			out = append(out, arch)
		}
	}
	return out
}
