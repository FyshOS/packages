package repo

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"packages.fyshos.com/cmd/internal/deb"
)

// Entry is one .deb in the pool together with the component it belongs to.
type Entry struct {
	*deb.Package
	Component string
}

// PoolPath returns the archive-relative home for a package, following the
// usual Debian pool layout: pool/<component>/<prefix>/<source>/<file>.deb.
func PoolPath(component string, p *deb.Package) string {
	source := p.Source
	if source == "" {
		source = p.Name
	}
	prefix := source[:1]
	if strings.HasPrefix(source, "lib") && len(source) > 3 {
		prefix = source[:4]
	}
	return path.Join("pool", component, prefix, source, p.BaseName())
}

// Add copies a .deb into the pool and returns its entry. An identical path is
// overwritten, which is how a rebuilt package of the same version is replaced.
func (c *Config) Add(component, file string) (*Entry, error) {
	if !c.HasComponent(component) {
		return nil, fmt.Errorf("unknown component %q, expected one of %s",
			component, strings.Join(c.Components, ", "))
	}
	pkg, err := deb.Open(file)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(file), err)
	}
	if !c.knownArch(pkg.Arch) {
		return nil, fmt.Errorf("%s: architecture %q is not in repo.json", pkg.Name, pkg.Arch)
	}

	pkg.Filename = PoolPath(component, pkg)
	dest := c.Path(filepath.FromSlash(pkg.Filename))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(dest, raw, 0o644); err != nil {
		return nil, err
	}
	return &Entry{Package: pkg, Component: component}, nil
}

// Remove deletes every pool file matching name, and optionally version and
// arch when they are non-empty. It returns the archive-relative paths removed.
func (c *Config) Remove(name, version, arch string) ([]string, error) {
	entries, err := c.Scan()
	if err != nil {
		return nil, err
	}

	var removed []string
	for _, e := range entries {
		if e.Name != name {
			continue
		}
		if version != "" && e.Version != version {
			continue
		}
		if arch != "" && e.Arch != arch {
			continue
		}
		if err := os.Remove(c.Path(filepath.FromSlash(e.Filename))); err != nil {
			return removed, err
		}
		removed = append(removed, e.Filename)
	}
	pruneEmpty(c.Path("pool"))
	return removed, nil
}

// Scan walks the pool and parses every .deb it finds. Packages are returned
// sorted by name, then version, then architecture.
func (c *Config) Scan() ([]*Entry, error) {
	root := c.Path("pool")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	}

	var out []*Entry
	err := filepath.WalkDir(root, func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(name, ".deb") {
			return err
		}
		pkg, err := deb.Open(name)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		rel, err := filepath.Rel(c.Path(), name)
		if err != nil {
			return err
		}
		pkg.Filename = filepath.ToSlash(rel)
		out = append(out, &Entry{Package: pkg, Component: componentOf(pkg.Filename)})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Version != b.Version {
			return a.Version < b.Version
		}
		return a.Arch < b.Arch
	})
	return out, nil
}

// componentOf reads the component out of a pool/<component>/... path.
func componentOf(filename string) string {
	parts := strings.Split(filename, "/")
	if len(parts) > 2 && parts[0] == "pool" {
		return parts[1]
	}
	return "main"
}

func (c *Config) knownArch(arch string) bool {
	if arch == "all" {
		return true
	}
	for _, a := range c.Architectures {
		if a == arch {
			return true
		}
	}
	return false
}

// pruneEmpty removes directories left behind by a removal, deepest first.
func pruneEmpty(root string) {
	var dirs []string
	filepath.WalkDir(root, func(name string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && name != root {
			dirs = append(dirs, name)
		}
		return nil
	})
	for i := len(dirs) - 1; i >= 0; i-- {
		if entries, err := os.ReadDir(dirs[i]); err == nil && len(entries) == 0 {
			os.Remove(dirs[i])
		}
	}
}
