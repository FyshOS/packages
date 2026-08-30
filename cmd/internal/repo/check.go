package repo

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// Check verifies the published metadata against the files on disk and returns
// a list of problems. An empty result means apt will be happy with the archive.
func (c *Config) Check() ([]string, error) {
	var problems []string

	for _, suite := range c.Suites {
		dist := c.Path("dists", suite)
		release, err := os.ReadFile(filepath.Join(dist, "Release"))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v — run `fyshpkg index`", suite, err))
			continue
		}
		problems = append(problems, c.checkRelease(dist, suite, string(release))...)

		for _, component := range c.Components {
			for _, arch := range c.BinaryArchitectures() {
				rel := path.Join(component, "binary-"+arch, "Packages")
				found, err := c.checkPackages(filepath.Join(dist, filepath.FromSlash(rel)))
				if err != nil {
					problems = append(problems, fmt.Sprintf("%s/%s: %v", suite, rel, err))
					continue
				}
				problems = append(problems, found...)
			}
		}

		if c.SigningKey != "" {
			for _, name := range []string{"InRelease", "Release.gpg"} {
				if _, err := os.Stat(filepath.Join(dist, name)); err != nil {
					problems = append(problems, fmt.Sprintf("%s: %s is missing", suite, name))
				}
			}
		}
	}
	return problems, nil
}

// checkRelease confirms every index listed under SHA256 exists and matches.
func (c *Config) checkRelease(dist, suite, release string) []string {
	var problems []string
	inSHA := false
	for _, line := range strings.Split(release, "\n") {
		if !strings.HasPrefix(line, " ") {
			inSHA = strings.HasPrefix(line, "SHA256:")
			continue
		}
		if !inSHA {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		want, size, name := fields[0], fields[1], fields[2]

		raw, err := os.ReadFile(filepath.Join(dist, filepath.FromSlash(name)))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: Release lists missing %s", suite, name))
			continue
		}
		if got := sha256.Sum256(raw); hex.EncodeToString(got[:]) != want {
			problems = append(problems, fmt.Sprintf("%s: %s does not match its Release checksum", suite, name))
		}
		if n, _ := strconv.Atoi(size); n != len(raw) {
			problems = append(problems, fmt.Sprintf("%s: %s does not match its Release size", suite, name))
		}
	}
	return problems
}

// checkPackages confirms every stanza points at a pool file with the right hash.
func (c *Config) checkPackages(name string) ([]string, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var problems []string
	var filename, want string
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scan.Scan() {
		line := scan.Text()
		switch {
		case strings.HasPrefix(line, "Filename: "):
			filename = strings.TrimPrefix(line, "Filename: ")
		case strings.HasPrefix(line, "SHA256: "):
			want = strings.TrimPrefix(line, "SHA256: ")
		case line == "" && filename != "":
			raw, err := os.ReadFile(c.Path(filepath.FromSlash(filename)))
			if err != nil {
				problems = append(problems, fmt.Sprintf("indexed but missing from the pool: %s", filename))
			} else if got := sha256.Sum256(raw); hex.EncodeToString(got[:]) != want {
				problems = append(problems, fmt.Sprintf("pool file has changed since indexing: %s", filename))
			}
			filename, want = "", ""
		}
	}
	return problems, scan.Err()
}
