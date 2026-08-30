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

	dir string // absolute project root, not serialised
}

// Load finds repo.json by walking up from the working directory, so fyshpkg
// works from anywhere inside the project.
func Load() (*Config, error) {
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
			return nil, fmt.Errorf("no %s found in this directory or any parent", ConfigName)
		}
		dir = parent
	}
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
	if key := os.Getenv("FYSHPKG_SIGNING_KEY"); key != "" {
		c.SigningKey = key
	}
	return c, nil
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
