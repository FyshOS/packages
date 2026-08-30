// Package deb reads binary Debian packages (.deb) without shelling out to dpkg.
//
// A .deb is an ar archive holding three members: debian-binary, a compressed
// control tarball and a compressed data tarball. Only the control tarball is
// interesting to an archive indexer, so that is all we unpack.
package deb

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// Field is a single control stanza field, keeping the order and any
// continuation lines exactly as they appeared in the package.
type Field struct {
	Name  string
	Value string
}

// Package is the metadata an archive needs to describe one .deb.
type Package struct {
	Fields []Field

	Name    string
	Source  string
	Version string
	Arch    string

	Size   int64
	MD5    string
	SHA1   string
	SHA256 string

	// Filename is the package's path relative to the archive root. It is set
	// by the repo package once the file has a home in the pool.
	Filename string
}

// Get returns the value of a control field, or "" when it is absent.
func (p *Package) Get(name string) string {
	for _, f := range p.Fields {
		if strings.EqualFold(f.Name, name) {
			return f.Value
		}
	}
	return ""
}

// ShortDescription is the first line of the Description field.
func (p *Package) ShortDescription() string {
	d, _, _ := strings.Cut(p.Get("Description"), "\n")
	return strings.TrimSpace(d)
}

// Open reads a .deb from disk and returns its control metadata along with the
// checksums an archive Packages file has to publish.
func Open(name string) (*Package, error) {
	raw, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

// Parse reads a .deb held in memory.
func Parse(raw []byte) (*Package, error) {
	members, err := arMembers(raw)
	if err != nil {
		return nil, err
	}

	var control []byte
	for _, m := range members {
		if !strings.HasPrefix(m.name, "control.tar") {
			continue
		}
		if control, err = controlFile(m.name, m.data); err != nil {
			return nil, err
		}
		break
	}
	if control == nil {
		return nil, fmt.Errorf("no control.tar member found")
	}

	p := &Package{Fields: parseStanza(control)}
	p.Name = p.Get("Package")
	p.Version = p.Get("Version")
	p.Arch = p.Get("Architecture")
	// "Source: foo (1.2-3)" names a source package with a differing version.
	p.Source, _, _ = strings.Cut(p.Get("Source"), " ")
	if p.Name == "" || p.Version == "" || p.Arch == "" {
		return nil, fmt.Errorf("control is missing Package, Version or Architecture")
	}

	p.Size = int64(len(raw))
	p.MD5 = hex.EncodeToString(sum(md5.New(), raw))
	p.SHA1 = hex.EncodeToString(sum(sha1.New(), raw))
	p.SHA256 = hex.EncodeToString(sum(sha256.New(), raw))
	return p, nil
}

// BaseName is the conventional pool filename for the package. Epochs are
// stripped because they are not part of a Debian archive file name.
func (p *Package) BaseName() string {
	version := p.Version
	if _, after, found := strings.Cut(version, ":"); found {
		version = after
	}
	return fmt.Sprintf("%s_%s_%s.deb", p.Name, version, p.Arch)
}

// Stanza renders the package as a Packages file entry, terminated by a blank
// line. Control fields keep their original order; the archive-specific fields
// are appended after them.
func (p *Package) Stanza() string {
	var b strings.Builder
	for _, f := range p.Fields {
		switch strings.ToLower(f.Name) {
		case "filename", "size", "md5sum", "sha1", "sha256":
			continue // recomputed below, never trusted from the package
		}
		fmt.Fprintf(&b, "%s: %s\n", f.Name, f.Value)
	}
	fmt.Fprintf(&b, "Filename: %s\n", p.Filename)
	fmt.Fprintf(&b, "Size: %d\n", p.Size)
	fmt.Fprintf(&b, "MD5sum: %s\n", p.MD5)
	fmt.Fprintf(&b, "SHA1: %s\n", p.SHA1)
	fmt.Fprintf(&b, "SHA256: %s\n", p.SHA256)
	b.WriteString("\n")
	return b.String()
}

type arMember struct {
	name string
	data []byte
}

func arMembers(raw []byte) ([]arMember, error) {
	const magic = "!<arch>\n"
	if len(raw) < len(magic) || string(raw[:len(magic)]) != magic {
		return nil, fmt.Errorf("not an ar archive")
	}

	var out []arMember
	for off := len(magic); off+60 <= len(raw); {
		hdr := raw[off : off+60]
		if string(hdr[58:60]) != "`\n" {
			return nil, fmt.Errorf("corrupt ar header at offset %d", off)
		}
		name := strings.TrimSuffix(strings.TrimSpace(string(hdr[0:16])), "/")
		size, err := strconv.ParseInt(strings.TrimSpace(string(hdr[48:58])), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("corrupt ar size at offset %d: %w", off, err)
		}
		off += 60
		if int64(len(raw)-off) < size {
			return nil, fmt.Errorf("ar member %q is truncated", name)
		}
		out = append(out, arMember{name: name, data: raw[off : off+int(size)]})
		off += int(size)
		if size%2 == 1 {
			off++ // members are padded to an even offset
		}
	}
	return out, nil
}

// controlFile decompresses a control tarball and returns its ./control member.
func controlFile(name string, data []byte) ([]byte, error) {
	r, err := decompress(path.Ext(name), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("control tarball has no control file")
		}
		if err != nil {
			return nil, err
		}
		if path.Clean(hdr.Name) != "control" {
			continue
		}
		return io.ReadAll(io.LimitReader(tr, 1<<20))
	}
}

func decompress(ext string, r io.Reader) (io.Reader, error) {
	switch ext {
	case ".gz":
		return gzip.NewReader(r)
	case ".xz":
		return xz.NewReader(r)
	case ".zst":
		d, err := zstd.NewReader(r)
		if err != nil {
			return nil, err
		}
		return d.IOReadCloser(), nil
	case ".tar", "":
		return r, nil
	}
	return nil, fmt.Errorf("unsupported control compression %q", ext)
}

// parseStanza reads one RFC822-style stanza, folding continuation lines into
// the value of the field above them.
func parseStanza(b []byte) []Field {
	var out []Field
	for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, " "), strings.HasPrefix(line, "\t"):
			if len(out) > 0 {
				out[len(out)-1].Value += "\n" + line
			}
		case strings.TrimSpace(line) == "":
			continue
		default:
			name, value, found := strings.Cut(line, ":")
			if !found {
				continue
			}
			out = append(out, Field{Name: strings.TrimSpace(name), Value: strings.TrimSpace(value)})
		}
	}
	return out
}

func sum(h hash.Hash, b []byte) []byte {
	h.Write(b)
	return h.Sum(nil)
}
