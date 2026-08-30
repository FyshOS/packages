package build

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDebianName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Fysh Notes", "fysh-notes"},
		{"FyshOS", "fyshos"},
		{"My  App!", "my-app"},
		{"Notes 2.0", "notes-2.0"},
		{"C++ Editor", "c++-editor"},
		{"-Leading-", "leading"},
	}
	for _, tt := range tests {
		if got := DebianName(tt.in); got != tt.want {
			t.Errorf("DebianName(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if !validName.MatchString(DebianName(tt.in)) {
			t.Errorf("DebianName(%q) = %q, which is not a valid package name", tt.in, DebianName(tt.in))
		}
	}
}

func TestDebianVersion(t *testing.T) {
	tests := []struct {
		version string
		build   int
		want    string
	}{
		{"1.2.3", 7, "1.2.3-7"},
		{"1.2.3", 0, "1.2.3-1"}, // fyne leaves Build unset until the first package
		{"", 3, "0.0.0-3"},      // no version in FyneApp.toml
	}
	for _, tt := range tests {
		if got := DebianVersion(tt.version, tt.build); got != tt.want {
			t.Errorf("DebianVersion(%q, %d) = %q, want %q", tt.version, tt.build, got, tt.want)
		}
	}
}

func TestSynopsis(t *testing.T) {
	app := &FyneApp{Details: AppDetails{Name: "Fysh Notes"}}
	if got := app.Synopsis(); got != "Fysh Notes" {
		t.Errorf("with no [LinuxAndBSD], Synopsis() = %q, want the app name", got)
	}

	app.LinuxAndBSD = &LinuxAndBSD{GenericName: "Note taking"}
	if got := app.Synopsis(); got != "Note taking" {
		t.Errorf("Synopsis() = %q, want the generic name", got)
	}

	app.LinuxAndBSD.Comment = "Take notes on the FyshOS desktop"
	if got := app.Synopsis(); got != "Take notes on the FyshOS desktop" {
		t.Errorf("Synopsis() = %q, want the comment", got)
	}
}

func TestRelocate(t *testing.T) {
	tests := []struct{ in, want string }{
		// fyne-cross writes usr/ at the top level of the bundle.
		{"usr/local/bin/fyshnotes", "usr/bin/fyshnotes"},
		{"usr/local/share/pixmaps/a.png", "usr/share/pixmaps/a.png"},
		{"Makefile", ""},
		{"usr", ""},
		{"usr/local", ""},
		// fyne wraps the same payload in a directory named after the binary.
		{"fyshnotes/usr/local/bin/fyshnotes", "usr/bin/fyshnotes"},
		{"fyshnotes/usr/local/share/pixmaps/a.png", "usr/share/pixmaps/a.png"},
		{"fyshnotes/Makefile", ""},
		{"fyshnotes", ""},
	}
	for _, tt := range tests {
		if got := relocate(tt.in, "/usr"); got != tt.want {
			t.Errorf("relocate(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if got := relocate("usr/local/bin/app", "/opt/fysh"); got != "opt/fysh/bin/app" {
		t.Errorf("relocate with a custom prefix = %q", got)
	}
}

func TestSetBuildNumber(t *testing.T) {
	const original = `Website = "https://fyshos.com"

[Details]
  Icon = "Icon.png"
  Name = "Fysh Notes"
  Version = "0.4.2"
  Build = 7
`
	dir := t.TempDir()
	name := filepath.Join(dir, MetadataName)
	if err := os.WriteFile(name, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := setBuildNumber(dir, 9); err != nil {
		t.Fatal(err)
	}
	app, err := LoadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if app.Details.Build != 9 {
		t.Errorf("Build = %d, want 9", app.Details.Build)
	}
	if app.Details.Name != "Fysh Notes" || app.Details.Version != "0.4.2" {
		t.Errorf("rewriting the build number disturbed the rest of the file: %+v", app.Details)
	}

	// Writing the same number again must leave the file untouched.
	before, _ := os.ReadFile(name)
	if err := setBuildNumber(dir, 9); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(name)
	if string(before) != string(after) {
		t.Error("rewriting an unchanged build number modified the file")
	}
}

func TestSetBuildNumberWhenMissing(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, MetadataName),
		[]byte("[Details]\n  Name = \"Fysh Notes\"\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	if err := setBuildNumber(dir, 3); err != nil {
		t.Fatal(err)
	}
	app, err := LoadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if app.Details.Build != 3 {
		t.Errorf("Build = %d, want 3", app.Details.Build)
	}
	if app.Details.Name != "Fysh Notes" {
		t.Errorf("Name = %q, want it preserved", app.Details.Name)
	}
}
