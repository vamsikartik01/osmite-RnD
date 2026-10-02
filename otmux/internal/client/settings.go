package client

import (
	"image/color"
	"os"
	"path/filepath"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/mattn/go-runewidth"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/config"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/keys"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/platform"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/version"
)

// Settings sections, in menu order.
const (
	secWorkspaces = iota
	secAppearance
	secKeys
	secUpdates
	secAbout
	numSections
)

var sectionNames = [numSections]string{"Workspaces", "Appearance", "Keys", "Updates", "About"}

var layoutLabels = map[string]string{
	config.LayoutSidebar: "Sidebar",
	config.LayoutTwoBars: "Two bars",
	config.LayoutCompact: "Compact",
}

var layoutHints = map[string]string{
	config.LayoutSidebar: "workspaces left, tabs on top",
	config.LayoutTwoBars: "tabs on top, workspaces below",
	config.LayoutCompact: "one bar",
}

// prefixChoices are the prefix keys offered in Settings › Keys.
var prefixChoices = []string{"ctrl+b", "ctrl+a", "ctrl+space", "ctrl+g"}

// settings is the settings panel (prefix + s): a menu of sections on the
// left, the selected section on the right. All fields are guarded by
// Client.mu.
type settings struct {
	section int
	inMenu  bool // keyboard focus is on the menu rather than the content
	sel     [numSections]int
	add     *addWorkspace // the "add workspace" form, while open
	note    string        // short feedback, e.g. "Saved"

	// Geometry from the last render, for mouse hits.
	box            box
	menuX, menuY   int
	rowsX, rowsY   int
	rowsW, visible int
}

// addWorkspace is the two-step form for saving a workspace.
type addWorkspace struct {
	step int // 0: name, 1: folder
	name []rune
	path []rune
}

// settingsRow is one selectable line in a section.
type settingsRow struct {
	left, right string
	current     bool // e.g. the theme in use
	run         func(c *Client) (keys.Action, bool)
	remove      func(c *Client) // if set, the row has a ✕ button and d removes it
}

// removeButton is drawn at the right end of removable rows.
const removeButton = " ✕ "

func (c *Client) openSettings() {
	c.mu.Lock()
	c.overlay = nil
	c.settings = &settings{inMenu: true}
	c.mu.Unlock()
}

// rows returns the selectable lines of the current section. Called with
// c.mu held.
func (c *Client) settingsRows(sec int) []settingsRow {
	var rows []settingsRow
	switch sec {
	case secWorkspaces:
		for _, p := range c.cfg.Workspaces {
			p := p
			rows = append(rows, settingsRow{left: p.Name, right: shortPath(p.Path),
				run: func(c *Client) (keys.Action, bool) {
					return keys.Action{Name: protocol.ActionNewWorkspace, Arg: p.Name, Dir: p.Path}, true
				},
				remove: func(c *Client) { c.removeProfile(p.Name) },
			})
		}
		rows = append(rows, settingsRow{left: "+ Add workspace…", run: func(c *Client) (keys.Action, bool) {
			c.settings.add = &addWorkspace{}
			return keys.Action{}, false
		}})
	case secAppearance:
		for _, t := range Themes {
			t := t
			rows = append(rows, settingsRow{left: t.Name, current: t.Name == c.theme.Name, run: func(c *Client) (keys.Action, bool) {
				c.theme = t
				c.cfg.Theme = t.Name
				c.saveConfig()
				return keys.Action{}, false
			}})
		}
		colours := "theme"
		if c.cfg.TerminalColors {
			colours = "terminal's own"
		}
		rows = append(rows, settingsRow{left: "Pane colours", right: colours, run: func(c *Client) (keys.Action, bool) {
			c.cfg.TerminalColors = !c.cfg.TerminalColors
			c.saveConfig()
			return keys.Action{}, false
		}})
		for _, l := range config.Layouts {
			l := l
			rows = append(rows, settingsRow{left: "Layout  " + layoutLabels[l], right: layoutHints[l], current: l == c.cfg.LayoutName(), run: func(c *Client) (keys.Action, bool) {
				c.cfg.SetLayout(l)
				c.saveConfig()
				return keys.Action{}, false
			}})
		}
	case secKeys:
		for _, p := range prefixChoices {
			p := p
			rows = append(rows, settingsRow{left: "Prefix  " + keys.Label(p), current: p == c.keys.Keymap().Prefix, run: func(c *Client) (keys.Action, bool) {
				if c.prefixLocked {
					c.settings.note = "OTMUX_PREFIX is set; it overrides this"
					return keys.Action{}, false
				}
				c.keys = keys.NewMachine(keys.Default(p))
				c.cfg.Prefix = p
				c.saveConfig()
				return keys.Action{}, false
			}})
		}
	case secUpdates:
		state := "on"
		if !c.cfg.AutoUpdateOn() {
			state = "off"
		}
		rows = append(rows, settingsRow{left: "Auto update", right: state, run: func(c *Client) (keys.Action, bool) {
			c.cfg.SetAutoUpdate(!c.cfg.AutoUpdateOn())
			c.saveConfig()
			return keys.Action{}, false
		}})
		label := "Update now"
		if c.updateBusy {
			label = "Update now  (checking…)"
		}
		rows = append(rows, settingsRow{left: label, run: func(c *Client) (keys.Action, bool) {
			go c.checkUpdates(true)
			return keys.Action{}, false
		}})
	}
	return rows
}

// settingsInfo is the non-selectable text shown above a section's rows.
func (c *Client) settingsInfo(sec int) []string {
	switch sec {
	case secWorkspaces:
		return []string{
			"Saved workspaces open in their folder.",
			"Find them in the sidebar, the picker (w) and search.",
			"enter opens it  ·  d or ✕ removes it from the list",
			"(removing never closes a running workspace)",
		}
	case secAppearance:
		return []string{
			"Themes colour the whole window: bars, panels and panes.",
			"Narrow windows fall back from Sidebar to Two bars automatically.",
		}
	case secKeys:
		p := keys.Label(c.keys.Keymap().Prefix)
		return []string{
			"The prefix starts every shortcut.",
			"Press " + p + " to see the key guide, " + p + " Space for all commands.",
		}
	case secUpdates:
		return c.updateInfoLines()
	case secAbout:
		return []string{
			"otmux " + version.Version,
			"",
			"Settings  " + config.Path(),
			"Socket    " + platform.SocketPath(),
			"Log       " + platform.LogPath(),
		}
	}
	return nil
}

func shortPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(strings.ToLower(p), strings.ToLower(home)) {
		return "~" + p[len(home):]
	}
	return p
}

func (c *Client) saveConfig() {
	if err := c.cfg.Save(); err != nil && c.settings != nil {
		c.settings.note = "Could not save: " + err.Error()
	} else if c.settings != nil {
		c.settings.note = "Saved"
	}
}

// settingsKey handles a key while the panel is open. It returns an action to
// run after the panel closes (e.g. opening a workspace).
func (c *Client) settingsKey(k uv.KeyPressEvent) (keys.Action, bool) {
	if keys.IsModifierOnly(k) {
		return keys.Action{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.settings
	st.note = ""

	if st.add != nil {
		return c.addWorkspaceKey(k)
	}
	rows := c.settingsRows(st.section)
	switch {
	case k.MatchString("esc", "ctrl+c", "q"):
		c.settings = nil
	case k.MatchString("left", "shift+tab"):
		st.inMenu = true
	case k.MatchString("right", "tab"):
		if len(rows) > 0 {
			st.inMenu = false
		}
	case k.MatchString("up", "k"):
		if st.inMenu {
			st.section = (st.section - 1 + numSections) % numSections
		} else if len(rows) > 0 {
			st.sel[st.section] = (st.sel[st.section] - 1 + len(rows)) % len(rows)
		}
	case k.MatchString("down", "j"):
		if st.inMenu {
			st.section = (st.section + 1) % numSections
		} else if len(rows) > 0 {
			st.sel[st.section] = (st.sel[st.section] + 1) % len(rows)
		}
	case k.MatchString("enter", "space"):
		if st.inMenu {
			if len(rows) > 0 {
				st.inMenu = false
			}
			return keys.Action{}, false
		}
		return c.activateRow(rows)
	case k.MatchString("d", "delete", "backspace"):
		if i := st.sel[st.section]; !st.inMenu && i < len(rows) && rows[i].remove != nil {
			rows[i].remove(c)
			st.sel[st.section] = max(i-1, 0)
		}
	}
	return keys.Action{}, false
}

// activateRow runs the selected row. Called with c.mu held.
func (c *Client) activateRow(rows []settingsRow) (keys.Action, bool) {
	st := c.settings
	i := st.sel[st.section]
	if i < 0 || i >= len(rows) {
		return keys.Action{}, false
	}
	act, ok := rows[i].run(c)
	if ok {
		c.settings = nil
	}
	return act, ok
}

// addWorkspaceKey edits the add-workspace form. Called with c.mu held.
func (c *Client) addWorkspaceKey(k uv.KeyPressEvent) (keys.Action, bool) {
	st := c.settings
	f := st.add
	field := &f.name
	if f.step == 1 {
		field = &f.path
	}
	switch {
	case k.MatchString("esc", "ctrl+c"):
		st.add = nil
	case k.MatchString("enter"):
		name := strings.TrimSpace(string(f.name))
		if f.step == 0 {
			if name == "" {
				st.note = "Give the workspace a name"
				return keys.Action{}, false
			}
			f.step = 1
			if len(f.path) == 0 {
				wd, _ := os.Getwd()
				f.path = []rune(wd)
			}
			return keys.Action{}, false
		}
		path := filepath.Clean(strings.TrimSpace(string(f.path)))
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			st.note = "That folder doesn't exist"
			return keys.Action{}, false
		}
		c.cfg.PutProfile(config.Profile{Name: name, Path: path})
		c.saveConfig()
		st.add = nil
	case k.MatchString("backspace"):
		if n := len(*field); n > 0 {
			*field = (*field)[:n-1]
		}
	case k.MatchString("ctrl+u"):
		*field = (*field)[:0]
	case k.Text != "":
		*field = append(*field, []rune(k.Text)...)
	}
	return keys.Action{}, false
}

// settingsClick handles a click while the panel is open.
func (c *Client) settingsClick(x, y int) (keys.Action, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.settings
	if !st.box.contains(x, y) {
		c.settings = nil
		return keys.Action{}, false
	}
	if st.add != nil {
		return keys.Action{}, false
	}
	if i := y - st.menuY; x < st.rowsX-2 && i >= 0 && i < numSections {
		st.section, st.inMenu = i, true
		return keys.Action{}, false
	}
	rows := c.settingsRows(st.section)
	if i := y - st.rowsY; x >= st.rowsX && i >= 0 && i < len(rows) && i < st.visible {
		st.sel[st.section], st.inMenu = i, false
		if rows[i].remove != nil && x >= st.rowsX+st.rowsW-runewidth.StringWidth(removeButton)-1 {
			rows[i].remove(c)
			st.sel[st.section] = max(i-1, 0)
			return keys.Action{}, false
		}
		return c.activateRow(rows)
	}
	return keys.Action{}, false
}

// drawSettings draws the panel and returns where the cursor goes (or -1).
//
//	╭─ Settings ─────────────────────────────────────────────╮
//	│                                                        │
//	│  Workspaces    │  Saved workspaces open in their folder.│
//	│  Appearance    │                                       │
//	│  Keys          │  api              ~/src/api           │
//	│  About         │  + Add workspace…                     │
//	│                                                        │
//	│  ↑↓ move   ←→ menu / list   enter select   esc close   │
//	╰────────────────────────────────────────────────────────╯
func (c *Client) drawSettings(s uv.Screen, st *settings) (int, int) {
	t := c.theme
	panel := uv.Style{Fg: t.Text, Bg: t.Surface}
	frame := uv.Style{Fg: t.Raised, Bg: t.Surface}
	w := min(86, c.cols-4)
	h := min(22, c.rows-2)
	if w < 40 || h < 10 {
		return -1, -1
	}
	bx, by := (c.cols-w)/2, max((c.rows-1-h)/3, 0)
	st.box = box{bx, by, w, h}
	for yy := by; yy < by+h; yy++ {
		fill(s, bx, yy, w, panel)
	}
	put(s, bx, by, "╭"+strings.Repeat("─", w-2)+"╮", frame)
	put(s, bx, by+h-1, "╰"+strings.Repeat("─", w-2)+"╯", frame)
	for yy := by + 1; yy < by+h-1; yy++ {
		put(s, bx, yy, "│", frame)
		put(s, bx+w-1, yy, "│", frame)
	}
	put(s, bx+2, by, " Settings ", uv.Style{Fg: t.Text, Bg: t.Surface, Attrs: uv.AttrBold})

	// Menu.
	menuW := 18
	st.menuX, st.menuY = bx+2, by+2
	for i, name := range sectionNames {
		y := st.menuY + i
		style := uv.Style{Fg: t.Muted, Bg: t.Surface}
		if i == st.section {
			style = uv.Style{Fg: t.Text, Bg: t.Raised, Attrs: uv.AttrBold}
		}
		fill(s, st.menuX, y, menuW-2, uv.Style{Bg: style.Bg})
		if i == st.section && st.inMenu {
			put(s, st.menuX, y, "›", uv.Style{Fg: t.Accent, Bg: style.Bg, Attrs: uv.AttrBold})
		}
		put(s, st.menuX+2, y, name, style)
	}
	for yy := by + 1; yy < by+h-2; yy++ {
		put(s, bx+menuW+1, yy, "│", frame)
	}

	// Content.
	cx := bx + menuW + 4
	cw := w - menuW - 6
	y := by + 2
	put(s, cx, y, sectionNames[st.section], uv.Style{Fg: t.Text, Bg: t.Surface, Attrs: uv.AttrBold})
	y += 2
	for _, line := range c.settingsInfo(st.section) {
		for _, part := range wrap(line, cw) {
			put(s, cx, y, part, uv.Style{Fg: t.Muted, Bg: t.Surface})
			y++
		}
	}
	y++

	cursorX, cursorY := -1, -1
	if st.add != nil {
		f := st.add
		label := func(yy int, name string, val []rune, active bool) {
			put(s, cx, yy, name, uv.Style{Fg: t.Muted, Bg: t.Surface})
			fs := uv.Style{Fg: t.Text, Bg: t.Raised}
			fill(s, cx+10, yy, cw-10, uv.Style{Bg: t.Raised})
			end := put(s, cx+11, yy, clipLeft(string(val), cw-13), fs)
			if active {
				cursorX, cursorY = end, yy
			}
		}
		put(s, cx, y, "Add workspace", uv.Style{Fg: t.Accent, Bg: t.Surface, Attrs: uv.AttrBold})
		label(y+2, "Name", f.name, f.step == 0)
		if f.step == 1 {
			label(y+3, "Folder", f.path, true)
		}
		put(s, cx, y+5, "enter next  ·  esc cancel", uv.Style{Fg: t.Faint, Bg: t.Surface})
	} else {
		rows := c.settingsRows(st.section)
		st.rowsX, st.rowsY, st.rowsW = cx-1, y, cw+2
		st.visible = max(by+h-3-y, 0)
		for i, r := range rows {
			if i >= st.visible {
				break
			}
			selected := !st.inMenu && i == st.sel[st.section]
			bg, fg, dim := t.Surface, t.Text, t.Faint
			if selected {
				bg, dim = t.Raised, t.Muted
			}
			fill(s, st.rowsX, y+i, st.rowsW, uv.Style{Bg: bg})
			if selected {
				put(s, st.rowsX, y+i, "›", uv.Style{Fg: t.Accent, Bg: bg, Attrs: uv.AttrBold})
			}
			left := r.left
			swatch := st.section == secAppearance && i < len(Themes)
			if swatch {
				left = "      " + left
			}
			if r.current {
				left += "  ✓"
			}
			end := cx + cw // right edge for the right-hand text
			if r.remove != nil {
				end -= runewidth.StringWidth(removeButton) + 1
				put(s, end+1, y+i, removeButton, uv.Style{Fg: fg, Bg: bgOr(selected, t.Raised, bg), Attrs: uv.AttrBold})
			}
			right := runewidth.Truncate(r.right, max(end-cx-runewidth.StringWidth(left)-3, 0), "…")
			rw := runewidth.StringWidth(right)
			put(s, cx, y+i, runewidth.Truncate(left, end-cx-rw-2, "…"), uv.Style{Fg: fg, Bg: bg, Attrs: boolAttr(r.current, uv.AttrBold)})
			if swatch {
				drawSwatch(s, cx, y+i, Themes[i])
			}
			if right != "" {
				put(s, end-rw, y+i, right, uv.Style{Fg: dim, Bg: bg})
			}
		}
	}

	footer := "↑↓ move   ←→ menu / list   enter select   esc close"
	if rows := c.settingsRows(st.section); st.add == nil && !st.inMenu {
		if i := st.sel[st.section]; i < len(rows) && rows[i].remove != nil {
			footer = "enter open   d remove   ←→ menu / list   esc close"
		}
	}
	if st.note != "" {
		footer = st.note
	}
	if st.note != "" {
		put(s, bx+3, by+h-2, runewidth.Truncate(footer, w-6, "…"), uv.Style{Fg: t.Accent, Bg: t.Surface})
	} else {
		drawKeyHints(s, bx+3, by+h-2, footer, t, t.Surface)
	}
	return cursorX, cursorY
}

// drawSwatch shows a theme's colours as a little strip of four cells.
func drawSwatch(s uv.Screen, x, y int, th Theme) {
	for i, col := range []color.Color{th.Bar, th.Raised, th.Accent, th.Attn} {
		s.SetCell(x+i, y, &uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: col}})
	}
}

// removeProfile forgets a saved workspace. A running workspace of that name
// keeps running. Called with c.mu held.
func (c *Client) removeProfile(name string) {
	c.cfg.RemoveProfile(name)
	c.saveConfig()
	if c.settings != nil && c.settings.note == "Saved" {
		c.settings.note = "Removed " + name
	}
}

func bgOr(cond bool, a, b color.Color) color.Color {
	if cond {
		return b
	}
	return a
}

func boolAttr(on bool, a uint8) uint8 {
	if on {
		return a
	}
	return 0
}

// wrap breaks text into lines of at most width cells, at spaces where it
// can. An empty line stays one empty line.
func wrap(text string, width int) []string {
	if width <= 0 || runewidth.StringWidth(text) <= width {
		return []string{text}
	}
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case runewidth.StringWidth(line+" "+word) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
		for runewidth.StringWidth(line) > width { // a single over-long word
			cut := runewidth.Truncate(line, width, "")
			lines = append(lines, cut)
			line = strings.TrimPrefix(line, cut)
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
