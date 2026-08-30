// Command fyshpkg manages the Debian archive that packages.fyshos.com serves.
//
// The archive lives in repo/ next to the Hugo site. Every command works on the
// project containing repo.json, found by walking up from the working directory.
//
//	fyshpkg package ./myapp      build a .deb from Fyne source and add it
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

	"packages.fyshos.com/cmd/internal/build"
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
	case "package", "pkg":
		err = pkg(args)
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
  fyshpkg package [flags] [source-dir]       build a Fyne app and add it
  fyshpkg add [-c component] <file.deb>...   add packages to the pool
  fyshpkg rm [-v version] [-a arch] <name>   remove packages from the pool
  fyshpkg index                              rebuild dists/ from the pool
  fyshpkg list [-json]                       list published packages
  fyshpkg check                              verify metadata against the pool
  fyshpkg key [-o dir] [-n name]             export the public signing key

Run "fyshpkg package -h" for the packaging flags.

Configuration lives in repo.json, found by walking up from the working
directory. Set FYSHPKG_REPO to the archive directory to work from elsewhere —
building an application from its own source tree, most of all.

Set FYSHPKG_SIGNING_KEY to sign with a key other than the one in repo.json.
`)
}

func pkg(args []string) error {
	fs := flag.NewFlagSet("package", flag.ExitOnError)
	component := fs.String("c", "main", "archive component to add the package to")
	repoDir := fs.String("repo", "", "archive directory (default: $FYSHPKG_REPO, else found from the working directory)")
	name := fs.String("name", "", "Debian package name (default: derived from the app name)")
	version := fs.String("version", "", "Debian version (default: Version-Build from FyneApp.toml)")
	arches := fs.String("arch", "", "comma-separated architectures to build (default: those in repo.json)")
	section := fs.String("section", "", "archive section")
	priority := fs.String("priority", "", "package priority")
	maintainer := fs.String("maintainer", "", "maintainer name and address")
	depends := fs.String("depends", "", "comma-separated runtime dependencies, replacing the repo.json defaults")
	description := fs.String("description", "", "one-line description (default: Comment from FyneApp.toml)")
	homepage := fs.String("homepage", "", "project URL (default: Website from FyneApp.toml)")
	prefix := fs.String("prefix", "/usr", "install prefix inside the package")
	tags := fs.String("tags", "", "comma-separated Go build tags")
	release := fs.Bool("release", false, "build in release mode")
	out := fs.String("o", "", "keep the built .deb in this directory")
	noAdd := fs.Bool("no-add", false, "build the .deb but do not add it to the archive")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: fyshpkg package [flags] [source-dir]

Cross-builds the Fyne application in source-dir (default: the working
directory) with fyne-cross, wraps each architecture as a .deb with dpkg-deb,
and adds them all to the archive. Identity and version come from FyneApp.toml;
the control fields Fyne has nowhere to record come from the package section of
repo.json.

The application usually lives outside the archive, so say where the archive is
— export FYSHPKG_REPO once in your shell profile, or pass -repo:

  export FYSHPKG_REPO=~/Code/FyshOS/packages.fyshos.com
  cd ~/Code/FyshOS/notes && fyshpkg package

  fyshpkg package -repo ~/Code/FyshOS/packages.fyshos.com

Every architecture in a release shares one build number, which becomes the
Debian revision, and FyneApp.toml is left holding the number that shipped.

fyne-cross builds in containers, so Docker or Podman has to be running.

Flags:
`)
		fs.PrintDefaults()
	}
	fs.Parse(args)
	if fs.NArg() > 1 {
		return fmt.Errorf("package takes at most one source directory")
	}

	c, err := loadRepo(*repoDir)
	if err != nil {
		return err
	}

	source := fs.Arg(0)
	if source == "" {
		source = "."
	}

	// With no -o the .deb is a means to an end, so it is built somewhere
	// temporary and cleaned up once it is safely in the pool.
	outputDir := *out
	if outputDir == "" {
		if *noAdd {
			outputDir = "."
		} else {
			temp, err := os.MkdirTemp("", "fyshpkg-deb-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(temp)
			outputDir = temp
		}
	}

	opts := build.Options{
		SourceDir:   source,
		OutputDir:   outputDir,
		Arches:      c.BinaryArchitectures(),
		Name:        *name,
		Version:     *version,
		Section:     firstSet(*section, c.Package.Section),
		Priority:    firstSet(*priority, c.Package.Priority),
		Maintainer:  firstSet(*maintainer, c.Package.Maintainer),
		Depends:     c.Package.Depends,
		Description: *description,
		Homepage:    *homepage,
		Prefix:      *prefix,
		Release:     *release,
		Tags:        *tags,
		Log:         func(format string, args ...any) { fmt.Printf(format+"\n", args...) },
	}
	if *depends != "" {
		opts.Depends = splitList(*depends)
	}
	if *arches != "" {
		opts.Arches = splitList(*arches)
	}

	built, err := build.Package(opts)
	if err != nil {
		return err
	}

	for _, result := range built {
		if *noAdd || *out != "" {
			fmt.Println("wrote", result.Path)
		}
		if *noAdd {
			continue
		}
		entry, err := c.Add(*component, result.Path)
		if err != nil {
			return err
		}
		fmt.Printf("added %s %s (%s) -> %s\n", entry.Name, entry.Version, entry.Arch, entry.Filename)
	}
	if *noAdd {
		return nil
	}
	return reindex(c)
}

// loadRepo opens the archive named by the flag, or lets repo.Load work it out
// from FYSHPKG_REPO and the working directory.
func loadRepo(dir string) (*repo.Config, error) {
	if dir != "" {
		return repo.LoadFrom(dir)
	}
	return repo.Load()
}

// firstSet returns the first non-empty value, so a flag beats repo.json.
func firstSet(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// splitList parses a comma-separated flag value, ignoring blank entries.
func splitList(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
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
