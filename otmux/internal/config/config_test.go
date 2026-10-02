package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	t.Setenv("OTMUX_CONFIG", filepath.Join(t.TempDir(), "c.json"))

	c, err := Load()
	if err != nil || c.LayoutName() != LayoutSidebar || len(c.Workspaces) != 0 {
		t.Fatalf("defaults: %+v, %v", c, err)
	}
	c.Theme = "calcite"
	c.SetLayout(LayoutCompact)
	c.PutProfile(Profile{Name: "api", Path: "/src/api"})
	c.PutProfile(Profile{Name: "web", Path: "/src/web"})
	c.PutProfile(Profile{Name: "API", Path: "/src/api2"}) // replaces, case-insensitive
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme != "calcite" || got.LayoutName() != LayoutCompact || len(got.Workspaces) != 2 {
		t.Fatalf("loaded %+v", got)
	}
	if p, ok := got.Profile("api"); !ok || p.Path != "/src/api2" {
		t.Fatalf("profile api = %+v %v", p, ok)
	}
	got.RemoveProfile("web")
	if _, ok := got.Profile("web"); ok || len(got.Workspaces) != 1 {
		t.Fatalf("after remove: %+v", got.Workspaces)
	}
}

func TestLegacySidebarOff(t *testing.T) {
	off := false
	c := &Config{Sidebar: &off}
	if c.LayoutName() != LayoutTwoBars {
		t.Fatalf("sidebar: false should become two bars, got %s", c.LayoutName())
	}
	if (&Config{Layout: "bogus"}).LayoutName() != LayoutSidebar {
		t.Fatal("unknown layouts fall back to the sidebar")
	}
}

func TestMigratesOldName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OTMUX_CONFIG", "")
	t.Setenv("APPDATA", home)         // Windows
	t.Setenv("XDG_CONFIG_HOME", home) // Linux
	t.Setenv("HOME", home)            // macOS
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Skip(err)
	}
	old := filepath.Join(dir, "oterm", "config.json")
	if err := os.MkdirAll(filepath.Dir(old), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte(`{"theme":"sugilite","workspaces":[{"name":"api","path":"/src/api"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil || c.Theme != "sugilite" || len(c.Workspaces) != 1 {
		t.Fatalf("old settings not picked up: %+v %v", c, err)
	}
	if _, err := os.Stat(Path()); err != nil {
		t.Fatalf("settings not copied to the new name: %v", err)
	}
}

// Saving must work while another process has the file open, which makes a
// plain rename fail on Windows.
func TestSaveWhileFileIsOpen(t *testing.T) {
	t.Setenv("OTMUX_CONFIG", filepath.Join(t.TempDir(), "c.json"))
	c := &Config{Theme: "graphite"}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(Path())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	c.Theme = "calcite"
	if err := c.Save(); err != nil {
		t.Fatalf("save while open: %v", err)
	}
	got, err := Load()
	if err != nil || got.Theme != "calcite" {
		t.Fatalf("after save while open: %+v %v", got, err)
	}
}
