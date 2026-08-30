package repo

import (
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Index regenerates every dists/ file from the current contents of the pool,
// signs the result when a key is configured and refreshes the website's data
// file. It is safe to run at any time: nothing outside dists/ is written.
func (c *Config) Index() ([]*Entry, error) {
	entries, err := c.Scan()
	if err != nil {
		return nil, err
	}

	for _, suite := range c.Suites {
		if err := c.indexSuite(suite, entries); err != nil {
			return nil, fmt.Errorf("%s: %w", suite, err)
		}
	}
	if err := c.writeSiteData(entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// indexSuite writes dists/<suite>, replacing whatever was there before.
func (c *Config) indexSuite(suite string, entries []*Entry) error {
	dist := c.Path("dists", suite)
	if err := os.RemoveAll(dist); err != nil {
		return err
	}

	// Paths recorded in Release are relative to dists/<suite>.
	var released []releasedFile
	for _, component := range c.Components {
		for _, arch := range c.BinaryArchitectures() {
			rel := path.Join(component, "binary-"+arch)
			if err := os.MkdirAll(filepath.Join(dist, filepath.FromSlash(rel)), 0o755); err != nil {
				return err
			}

			packages := renderPackages(entries, component, arch)
			files, err := writeIndex(dist, rel, packages)
			if err != nil {
				return err
			}
			released = append(released, files...)

			archRelease := c.archRelease(suite, component, arch)
			f, err := writeFile(dist, path.Join(rel, "Release"), []byte(archRelease))
			if err != nil {
				return err
			}
			released = append(released, f)
		}
	}

	release := c.release(suite, released)
	if err := os.WriteFile(filepath.Join(dist, "Release"), []byte(release), 0o644); err != nil {
		return err
	}
	return c.sign(dist)
}

// renderPackages builds the Packages file for one component and architecture.
// Architecture "all" packages are folded into every architecture so that older
// apt clients, which do not fetch binary-all, still see them.
func renderPackages(entries []*Entry, component, arch string) []byte {
	var b bytes.Buffer
	for _, e := range entries {
		if e.Component != component {
			continue
		}
		if e.Arch != arch && e.Arch != "all" {
			continue
		}
		b.WriteString(e.Stanza())
	}
	return b.Bytes()
}

type releasedFile struct {
	name   string // relative to dists/<suite>
	size   int64
	md5    string
	sha256 string
}

// writeIndex writes Packages and its gzip companion.
func writeIndex(dist, dir string, packages []byte) ([]releasedFile, error) {
	plain, err := writeFile(dist, path.Join(dir, "Packages"), packages)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if _, err := zw.Write(packages); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	gz, err := writeFile(dist, path.Join(dir, "Packages.gz"), buf.Bytes())
	if err != nil {
		return nil, err
	}
	return []releasedFile{plain, gz}, nil
}

func writeFile(dist, name string, content []byte) (releasedFile, error) {
	full := filepath.Join(dist, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return releasedFile{}, err
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		return releasedFile{}, err
	}
	return releasedFile{
		name:   name,
		size:   int64(len(content)),
		md5:    sum(md5.New(), content),
		sha256: sum(sha256.New(), content),
	}, nil
}

// archRelease is the small per-architecture Release file apt uses to confirm
// it downloaded the index it asked for.
func (c *Config) archRelease(suite, component, arch string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Archive: %s\n", suite)
	fmt.Fprintf(&b, "Component: %s\n", component)
	fmt.Fprintf(&b, "Origin: %s\n", c.Origin)
	fmt.Fprintf(&b, "Label: %s\n", c.Label)
	fmt.Fprintf(&b, "Architecture: %s\n", arch)
	return b.String()
}

// release renders dists/<suite>/Release, listing a checksum for every index.
func (c *Config) release(suite string, files []releasedFile) string {
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	now := time.Now().UTC()

	var b strings.Builder
	fmt.Fprintf(&b, "Origin: %s\n", c.Origin)
	fmt.Fprintf(&b, "Label: %s\n", c.Label)
	fmt.Fprintf(&b, "Suite: %s\n", suite)
	fmt.Fprintf(&b, "Codename: %s\n", suite)
	fmt.Fprintf(&b, "Date: %s\n", now.Format("Mon, 02 Jan 2006 15:04:05 UTC"))
	if c.ValidDays > 0 {
		until := now.AddDate(0, 0, c.ValidDays)
		fmt.Fprintf(&b, "Valid-Until: %s\n", until.Format("Mon, 02 Jan 2006 15:04:05 UTC"))
	}
	fmt.Fprintf(&b, "Architectures: %s\n", strings.Join(c.BinaryArchitectures(), " "))
	fmt.Fprintf(&b, "Components: %s\n", strings.Join(c.Components, " "))
	fmt.Fprintf(&b, "Description: %s\n", c.Description)

	b.WriteString("MD5Sum:\n")
	for _, f := range files {
		fmt.Fprintf(&b, " %s %16d %s\n", f.md5, f.size, f.name)
	}
	b.WriteString("SHA256:\n")
	for _, f := range files {
		fmt.Fprintf(&b, " %s %16d %s\n", f.sha256, f.size, f.name)
	}
	return b.String()
}

func sum(h hash.Hash, b []byte) string {
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}
