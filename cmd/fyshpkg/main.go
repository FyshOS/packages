// Command fyshpkg manages the Debian archive that packages.fyshos.com serves.
//
// The archive lives in repo/ next to the Hugo site. Every command works on the
// project containing repo.json, found by walking up from the working directory.
//
//	fyshpkg add build/*.deb      copy packages into the pool and reindex
//	fyshpkg rm fysh-desktop      drop a package from the pool and reindex
//	fyshpkg index                rebuild dists/ from whatever is in the pool
//	fyshpkg list                 show what the archive publishes
//	fyshpkg check                verify the metadata against the pool
//	fyshpkg key                  export the public signing key into static/
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"packages.fyshos.com/cmd/internal/repo"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	args := os.Args[2:]
	var err error
	switch os.Args[1] {
	case "add":
		err = add(args)
	case "rm", "remove":
		err = remove(args)
	case "index", "reindex":
		err = index(args)
	case "list", "ls":
		err = list(args)
	case "check", "verify":
		err = check(args)
	case "key":
		err = key(args)
	case "help", "-h", "--help":
		usage()
		return
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "fyshpkg:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `fyshpkg manages the FyshOS Debian archive in repo/.

Usage:
  fyshpkg add [-c component] <file.deb>...   add packages to the pool
  fyshpkg rm [-v version] [-a arch] <name>   remove packages from the pool
  fyshpkg index                              rebuild dists/ from the pool
  fyshpkg list [-json]                       list published packages
  fyshpkg check                              verify metadata against the pool
  fyshpkg key [-o dir] [-n name]             export the public signing key

Configuration lives in repo.json. Set FYSHPKG_SIGNING_KEY to sign with a key
other than the one recorded there.
`)
}

func add(args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	component := fs.String("c", "main", "archive component to add the packages to")
	fs.Parse(args)
	if fs.NArg() == 0 {
		return fmt.Errorf("add needs at least one .deb file")
	}

	c, err := repo.Load()
	if err != nil {
		return err
	}
	for _, file := range fs.Args() {
		entry, err := c.Add(*component, file)
		if err != nil {
			return err
		}
		fmt.Printf("added %s %s (%s) -> %s\n", entry.Name, entry.Version, entry.Arch, entry.Filename)
	}
	return reindex(c)
}

func remove(args []string) error {
	fs := flag.NewFlagSet("rm", flag.ExitOnError)
	version := fs.String("v", "", "only remove this version")
	arch := fs.String("a", "", "only remove this architecture")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("rm needs exactly one package name")
	}

	c, err := repo.Load()
	if err != nil {
		return err
	}
	removed, err := c.Remove(fs.Arg(0), *version, *arch)
	if err != nil {
		return err
	}
	if len(removed) == 0 {
		return fmt.Errorf("no package matched %q", fs.Arg(0))
	}
	for _, name := range removed {
		fmt.Println("removed", name)
	}
	return reindex(c)
}

func index(args []string) error {
	flag.NewFlagSet("index", flag.ExitOnError).Parse(args)
	c, err := repo.Load()
	if err != nil {
		return err
	}
	return reindex(c)
}

func reindex(c *repo.Config) error {
	entries, err := c.Index()
	if err != nil {
		return err
	}
	signed := "unsigned — apt will need [trusted=yes]"
	if c.SigningKey != "" {
		signed = "signed with " + c.SigningKey
	}
	fmt.Printf("indexed %d package(s) across %s, %s\n",
		len(entries), strings.Join(c.Suites, ", "), signed)
	return nil
}

func list(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "print the raw site data instead of a table")
	fs.Parse(args)

	c, err := repo.Load()
	if err != nil {
		return err
	}
	if *asJSON {
		raw, err := os.ReadFile(filepath.Join(c.Dir(), c.DataDir, "packages.json"))
		if err != nil {
			return fmt.Errorf("%w — run `fyshpkg index` first", err)
		}
		os.Stdout.Write(raw)
		return nil
	}

	entries, err := c.Scan()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println("the pool is empty")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PACKAGE\tVERSION\tARCH\tCOMPONENT\tSIZE")
	for _, e := range entries {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			e.Name, e.Version, e.Arch, e.Component, humanSize(e.Size))
	}
	return w.Flush()
}

func check(args []string) error {
	flag.NewFlagSet("check", flag.ExitOnError).Parse(args)
	c, err := repo.Load()
	if err != nil {
		return err
	}
	problems, err := c.Check()
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, " ", p)
		}
		return fmt.Errorf("%d problem(s) found — run `fyshpkg index`", len(problems))
	}
	fmt.Println("archive metadata matches the pool")
	return nil
}

func key(args []string) error {
	fs := flag.NewFlagSet("key", flag.ExitOnError)
	out := fs.String("o", "static", "directory to write the key into")
	name := fs.String("n", "fyshos-archive-keyring", "base name for the exported key")
	fs.Parse(args)

	c, err := repo.Load()
	if err != nil {
		return err
	}
	dir := *out
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(c.Dir(), dir)
	}
	written, err := c.ExportKey(dir, *name)
	for _, w := range written {
		if rel, relErr := filepath.Rel(c.Dir(), w); relErr == nil && !strings.HasPrefix(rel, "..") {
			w = rel
		}
		fmt.Println("wrote", w)
	}
	return err
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for size := n / unit; size >= unit; size /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
