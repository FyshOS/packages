package repo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SitePackage is one row of the package table on the website.
type SitePackage struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	Architecture  string `json:"architecture"`
	Component     string `json:"component"`
	Section       string `json:"section,omitempty"`
	Description   string `json:"description,omitempty"`
	Homepage      string `json:"homepage,omitempty"`
	Maintainer    string `json:"maintainer,omitempty"`
	Filename      string `json:"filename"`
	Size          int64  `json:"size"`
	InstalledSize int64  `json:"installedSize,omitempty"`
	SHA256        string `json:"sha256"`
}

// SiteData is the shape of data/packages.json.
type SiteData struct {
	Generated time.Time     `json:"generated"`
	Suites    []string      `json:"suites"`
	Packages  []SitePackage `json:"packages"`
}

// writeSiteData refreshes the JSON the Hugo site reads. Only the newest
// version of each name and architecture is listed; the archive itself still
// carries every version that is in the pool.
func (c *Config) writeSiteData(entries []*Entry) error {
	newest := map[string]*Entry{}
	for _, e := range entries {
		key := e.Name + "/" + e.Arch
		if cur, ok := newest[key]; !ok || CompareVersions(e.Version, cur.Version) > 0 {
			newest[key] = e
		}
	}

	data := SiteData{Generated: time.Now().UTC(), Suites: c.Suites, Packages: []SitePackage{}}
	for _, e := range newest {
		installed, _ := strconv.ParseInt(e.Get("Installed-Size"), 10, 64)
		data.Packages = append(data.Packages, SitePackage{
			Name:          e.Name,
			Version:       e.Version,
			Architecture:  e.Arch,
			Component:     e.Component,
			Section:       e.Get("Section"),
			Description:   e.ShortDescription(),
			Homepage:      e.Get("Homepage"),
			Maintainer:    e.Get("Maintainer"),
			Filename:      e.Filename,
			Size:          e.Size,
			InstalledSize: installed * 1024,
			SHA256:        e.SHA256,
		})
	}
	sort.Slice(data.Packages, func(i, j int) bool {
		a, b := data.Packages[i], data.Packages[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Architecture < b.Architecture
	})

	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Join(c.dir, c.DataDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "packages.json"), append(raw, '\n'), 0o644)
}

// CompareVersions implements the dpkg version ordering: epoch first, then
// upstream version, then Debian revision, each compared with the rule that
// '~' sorts before the end of a string and letters sort before punctuation.
func CompareVersions(a, b string) int {
	ae, au, ar := splitVersion(a)
	be, bu, br := splitVersion(b)

	if ae != be {
		if ae < be {
			return -1
		}
		return 1
	}
	if n := compareParts(au, bu); n != 0 {
		return n
	}
	return compareParts(ar, br)
}

func splitVersion(v string) (epoch int, upstream, revision string) {
	if before, after, found := strings.Cut(v, ":"); found {
		if n, err := strconv.Atoi(before); err == nil {
			epoch, v = n, after
		}
	}
	if i := strings.LastIndex(v, "-"); i >= 0 {
		return epoch, v[:i], v[i+1:]
	}
	return epoch, v, ""
}

// compareParts walks two version fragments, alternating between runs of
// non-digits compared by rank and runs of digits compared numerically.
func compareParts(a, b string) int {
	for len(a) > 0 || len(b) > 0 {
		i, j := 0, 0
		for i < len(a) && !isDigit(a[i]) {
			i++
		}
		for j < len(b) && !isDigit(b[j]) {
			j++
		}
		if n := compareAlpha(a[:i], b[:j]); n != 0 {
			return n
		}
		a, b = a[i:], b[j:]

		i, j = 0, 0
		for i < len(a) && isDigit(a[i]) {
			i++
		}
		for j < len(b) && isDigit(b[j]) {
			j++
		}
		na, _ := strconv.Atoi(a[:i])
		nb, _ := strconv.Atoi(b[:j])
		if na != nb {
			if na < nb {
				return -1
			}
			return 1
		}
		a, b = a[i:], b[j:]
	}
	return 0
}

func compareAlpha(a, b string) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var ra, rb int
		if i < len(a) {
			ra = rank(a[i])
		}
		if i < len(b) {
			rb = rank(b[i])
		}
		if ra != rb {
			if ra < rb {
				return -1
			}
			return 1
		}
	}
	return 0
}

// rank orders a single character: '~' before the end of string, then letters,
// then everything else.
func rank(c byte) int {
	switch {
	case c == '~':
		return -1
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z':
		return int(c)
	default:
		return int(c) + 256
	}
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
