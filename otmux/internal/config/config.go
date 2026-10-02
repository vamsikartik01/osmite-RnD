// Package config loads and saves the user's otmux settings: theme, prefix
// key, sidebar, and saved workspaces (a name plus the folder its shells start
// in). It is a small JSON file, edited through the settings panel
// (prefix + s) or by hand.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Profile is a saved workspace: opening it starts its shells in Path.
type Profile struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Layouts: where workspaces and tabs are shown.
const (
	LayoutSidebar = "sidebar" // workspaces in a left sidebar, tabs in the bottom bar (default)
	LayoutTwoBars = "twobars" // tabs in a top bar, workspaces in the bottom bar
	LayoutCompact = "compact" // everything in one bottom bar
)

// Layouts lists the layouts in the order settings offers them.
var Layouts = []string{LayoutSidebar, LayoutTwoBars, LayoutCompact}

// Config is the settings file.
type Config struct {
	Theme  string `json:"theme,omitempty"`
	Prefix string `json:"prefix,omitempty"`
	Layout string `json:"layout,omitempty"` // empty means LayoutSidebar
	// TerminalColors keeps panes in the terminal's own colours instead of
	// the theme's.
	TerminalColors bool      `json:"terminal_colors,omitempty"`
	Workspaces     []Profile `json:"workspaces,omitempty"`

	// Sidebar is the setting older versions wrote; false now means two bars.
	Sidebar *bool `json:"sidebar,omitempty"`
}

// Path is where the settings live: OTMUX_CONFIG if set, otherwise
// %AppData%\otmux\config.json on Windows, ~/Library/Application
// Support/otmux/config.json on macOS, and ~/.config/otmux/config.json on Linux.
func Path() string {
	if p := os.Getenv("OTMUX_CONFIG"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "otmux", "config.json")
}

// legacyPath is where the settings lived when otmux was called oterm.
func legacyPath() string {
	if os.Getenv("OTMUX_CONFIG") != "" {
		return ""
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "oterm", "config.json")
}

// Load reads the settings. A missing file is not an error: it means defaults.
// Settings saved under the old name (oterm) are picked up and copied over.
func Load() (*Config, error) {
	c := &Config{}
	data, err := os.ReadFile(Path())
	if errors.Is(err, fs.ErrNotExist) {
		if old := legacyPath(); old != "" {
			if legacy, lerr := os.ReadFile(old); lerr == nil && json.Unmarshal(legacy, c) == nil {
				_ = c.Save()
				return c, nil
			}
		}
		return &Config{}, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, c); err != nil {
		return &Config{}, err
	}
	return c, nil
}

// Save writes the settings, atomically when it can.
func (c *Config) Save() error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	// On Windows the rename fails while anything else has the file open
	// (antivirus, an editor, another otmux reading it), so retry briefly,
	// then fall back to writing in place.
	for range 10 {
		if err = os.Rename(tmp, p); err == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = os.Remove(tmp)
	if werr := os.WriteFile(p, data, 0o600); werr != nil {
		return fmt.Errorf("%w (and writing in place: %v)", err, werr)
	}
	return nil
}

// LayoutName returns the chosen layout; new installs get the sidebar.
func (c *Config) LayoutName() string {
	for _, l := range Layouts {
		if c.Layout == l {
			return l
		}
	}
	if c.Sidebar != nil && !*c.Sidebar {
		return LayoutTwoBars
	}
	return LayoutSidebar
}

// SetLayout chooses a layout.
func (c *Config) SetLayout(l string) {
	c.Layout = l
	c.Sidebar = nil
}

// Profile returns the saved workspace called name.
func (c *Config) Profile(name string) (Profile, bool) {
	for _, p := range c.Workspaces {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return Profile{}, false
}

// PutProfile adds or replaces a saved workspace.
func (c *Config) PutProfile(p Profile) {
	for i, q := range c.Workspaces {
		if strings.EqualFold(q.Name, p.Name) {
			c.Workspaces[i] = p
			return
		}
	}
	c.Workspaces = append(c.Workspaces, p)
}

// RemoveProfile deletes the saved workspace called name.
func (c *Config) RemoveProfile(name string) {
	out := c.Workspaces[:0]
	for _, p := range c.Workspaces {
		if !strings.EqualFold(p.Name, name) {
			out = append(out, p)
		}
	}
	c.Workspaces = out
}
