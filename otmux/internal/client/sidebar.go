package client

import (
	"fmt"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/mattn/go-runewidth"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/agents"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/config"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/keys"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
)

// sidebarWidth is the sidebar's width in cells; one more column holds the
// line that separates it from the panes.
const sidebarWidth = 26

// sidebarMinCols is the narrowest terminal that still shows the sidebar.
const sidebarMinCols = 90

// twoBarsMinRows is the shortest terminal that still gets a top tab bar.
const twoBarsMinRows = 12

// layout returns the layout in effect: the chosen one, falling back to two
// bars when the window is too narrow for the sidebar, and to one bar when
// it's too short for two. Called with c.mu held.
func (c *Client) layout() string {
	l := c.cfg.LayoutName()
	if l == config.LayoutSidebar && c.cols < sidebarMinCols {
		l = config.LayoutTwoBars
	}
	if l == config.LayoutTwoBars && c.rows < twoBarsMinRows {
		l = config.LayoutCompact
	}
	return l
}

// ox is how far right the panes start: past the sidebar, its edge and a
// column of space, if it's shown. Called with c.mu held.
func (c *Client) ox() int {
	if c.layout() == config.LayoutSidebar {
		return sidebarWidth + 2
	}
	return 0
}

// barX is where the tab bar and the top title row start: right after the
// sidebar's edge. Called with c.mu held.
func (c *Client) barX() int {
	if c.layout() == config.LayoutSidebar {
		return sidebarWidth + 1
	}
	return 0
}

// oy is how far down the panes start: below the tab bar and the title row
// of the top panes, if they're shown. Called with c.mu held.
func (c *Client) oy() int {
	if c.layout() == config.LayoutCompact {
		return 0
	}
	return 2
}

// paneCols is the width the daemon lays panes out in. Called with c.mu held.
func (c *Client) paneCols() int { return max(c.cols-c.ox(), 1) }

// daemonRows is the height to report to the daemon, which keeps one row for
// the bottom bar itself. Called with c.mu held.
func (c *Client) daemonRows() int { return max(c.rows-c.oy(), 2) }

// drawSidebar draws the sidebar on one alignment grid: the otmux mark on
// the tab bar's row, section headings at column 1, items straight below them
// with a blank row between, notes right-aligned, and the current item in a
// highlighted box. A watched tab takes two rows: its name, then the agent in
// it and its workspace. A line on its right edge separates the sidebar from
// the panes.
//
//	 otmux                    │
//	                          │
//	 WATCH                    │
//	 ⠹ claude-refactor working│
//	   Claude · api           │
//	                          │
//	 ● codex-tests    waiting │
//	   Codex · web            │
//	                          │
//	 WORKSPACES             + │
//	[  api                 3 ]│  current
//	[  ~/src/api             ]│
//	                          │
//	   web                 1  │
//	                          │  no saved folder
//	                          │
//	   lab                    │  saved, not open
//	   ~/src/lab              │
//	…                         │
//	   Settings     Ctrl+B s  │
func (c *Client) drawSidebar(s uv.Screen, h int) {
	t := c.theme
	w := sidebarWidth
	plain := uv.Style{Fg: t.Muted, Bg: t.Bar}
	faint := uv.Style{Fg: t.Faint, Bg: t.Bar}
	boxed := uv.Style{Fg: t.Text, Bg: t.Raised, Attrs: uv.AttrBold}
	edge := uv.Style{Fg: t.Border, Bg: t.Bar}
	for y := 0; y < h; y++ {
		fill(s, 0, y, w, uv.Style{Bg: t.Bar})
		put(s, w, y, "│", edge)
	}

	// row draws one line: text from column x, an optional right-aligned
	// note, the background across the full width, and a click target.
	row := func(y, x int, st uv.Style, text, note string, act *keys.Action) {
		fill(s, 0, y, w, uv.Style{Bg: st.Bg})
		nw := runewidth.StringWidth(note)
		put(s, x, y, runewidth.Truncate(text, w-x-nw-2, "…"), st)
		if note != "" {
			put(s, w-1-nw, y, note, uv.Style{Fg: t.Faint, Bg: st.Bg})
		}
		if act != nil {
			c.hits = append(c.hits, hit{x0: 0, x1: w, y: y, action: *act})
		}
	}
	heading := func(y int, text string) {
		put(s, 1, y, text, uv.Style{Fg: t.Faint, Bg: t.Bar, Attrs: uv.AttrBold})
	}
	bottom := h - 1 // settings sits on the last row
	prefix := keys.Label(c.keys.Keymap().Prefix)

	// The mark, in the logo's gradient, level with the tab bar.
	x := 1
	for i, r := range "otmux" {
		x = put(s, x, 0, string(r), uv.Style{Fg: gradient(t, float64(i)/4), Bg: t.Bar, Attrs: uv.AttrBold})
	}

	// Watch list, capped at half the sidebar so workspaces stay visible.
	// Each tab is two rows, with a blank row between tabs while they fit.
	y := 2
	heading(y, "WATCH")
	y++
	watchEnd := max(y+(bottom-y)/2, y+3)
	pinned := c.state.Pinned
	gap := 1
	if len(pinned)*3-1 > watchEnd-y {
		gap = 0
	}
	var current uint32
	if i := c.state.Active; i >= 0 && i < len(c.state.Tabs) {
		current = c.state.Tabs[i].ID
	}
	for i, p := range pinned {
		if y+2 > watchEnd && i < len(pinned)-1 || y+2 > bottom-1 {
			if y < bottom-1 {
				row(y, 3, faint, fmt.Sprintf("%d more  %s Tab", len(pinned)-i, prefix), "", nil)
				y++
			}
			gap = 0
			break
		}
		st := plain
		if p.TabID == current {
			st = boxed
		}
		act := &keys.Action{Name: protocol.ActionGotoTab, Arg: fmt.Sprint(p.TabID)}
		note := p.Status
		if note == protocol.StatusIdle {
			note = ""
		}
		row(y, 3, st, p.Name, note, act)
		glyph, gst := statusGlyph(t, p.Status)
		gst.Bg = st.Bg
		put(s, 1, y, glyph, gst)
		if p.Status == protocol.StatusWaiting {
			put(s, w-1-len(note), y, note, uv.Style{Fg: t.Attn, Bg: st.Bg})
		}
		y++

		// Second row: the agent, highlighted, then the workspace.
		row(y, 3, uv.Style{Bg: st.Bg}, "", "", act)
		x := 3
		if p.Agent != "" {
			x = put(s, x, y, agents.Name(p.Agent), uv.Style{Fg: t.Accent, Bg: st.Bg, Attrs: uv.AttrBold})
			x = put(s, x, y, " · ", uv.Style{Fg: t.Faint, Bg: st.Bg})
		}
		if end := w - 1; end-x > 1 {
			put(s, x, y, runewidth.Truncate(p.Workspace, end-x, "…"), uv.Style{Fg: t.Faint, Bg: st.Bg})
		}
		y += 1 + gap
	}
	if len(pinned) == 0 {
		row(y, 1, faint, prefix+" m to watch a tab", "", nil)
		y++
	} else if gap == 1 {
		y-- // the last tab's gap
	}

	// Workspaces, with + in the heading to add one: running ones, then saved
	// ones that aren't open (dimmer, and no tab count). A blank row between
	// them while they fit.
	y++
	if y >= bottom-1 {
		return
	}
	heading(y, "WORKSPACES")
	put(s, w-2, y, "+", uv.Style{Fg: t.Muted, Bg: t.Bar, Attrs: uv.AttrBold})
	c.hits = append(c.hits, hit{x0: w - 4, x1: w, y: y, action: keys.Action{Name: keys.ActionPromptNewWorkspace}})
	y++
	type wsRow struct {
		st               uv.Style
		name, note, path string
		act              keys.Action
	}
	paths := map[string]string{}
	for _, p := range c.cfg.Workspaces {
		paths[strings.ToLower(p.Name)] = p.Path
	}
	var rows []wsRow
	running := map[string]bool{}
	for _, ws := range c.state.Workspaces {
		running[strings.ToLower(ws.Name)] = true
		path := paths[strings.ToLower(ws.Name)]
		if ws.Name == c.state.Workspace {
			rows = append(rows, wsRow{boxed, ws.Name, fmt.Sprint(ws.Tabs), path, keys.Action{Name: keys.ActionChooseWorkspace}})
		} else {
			rows = append(rows, wsRow{plain, ws.Name, fmt.Sprint(ws.Tabs), path, keys.Action{Name: protocol.ActionSwitchWorkspace, Arg: ws.Name}})
		}
	}
	for _, p := range c.cfg.Workspaces {
		if !running[strings.ToLower(p.Name)] {
			rows = append(rows, wsRow{faint, p.Name, "", p.Path, keys.Action{Name: protocol.ActionNewWorkspace, Arg: p.Name, Dir: p.Path}})
		}
	}
	// Two rows each (name, then folder) and a blank row between while they
	// fit; then without the blank rows; then one row each.
	lines, gap := 2, 1
	switch room := bottom - 1 - y; {
	case len(rows)*3-1 <= room:
	case len(rows)*2 <= room:
		gap = 0
	default:
		lines, gap = 1, 0
	}
	for _, r := range rows {
		if y+lines > bottom-1 {
			break
		}
		row(y, 3, r.st, r.name, r.note, &r.act)
		if lines == 2 {
			row(y+1, 3, uv.Style{Bg: r.st.Bg}, "", "", &r.act)
			if r.path != "" {
				put(s, 3, y+1, clipLeft(shortPath(r.path), w-4), uv.Style{Fg: t.Faint, Bg: r.st.Bg})
			}
		}
		y += lines + gap
	}

	if bottom > y {
		row(bottom, 3, faint, "Settings", prefix+" s", &keys.Action{Name: keys.ActionSettings})
	}
}

// spinInterval is how long each frame of a working agent's spinner shows.
const spinInterval = 100 * time.Millisecond

// spinner is the animation shown while an agent works.
var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// now is the clock used for animation; tests replace it.
var now = time.Now

// anyWorking reports whether a watched tab's spinner is turning, so the
// screen needs redrawing to animate it. Called with c.mu held.
func (c *Client) anyWorking() bool {
	for _, p := range c.state.Pinned {
		if p.Status == protocol.StatusWorking {
			return true
		}
	}
	return false
}

// statusGlyph is the mark shown for a watched tab: a spinner while the
// agent works (accent), ● when it waits for you (attention), ○ idle.
func statusGlyph(t Theme, status string) (string, uv.Style) {
	switch status {
	case protocol.StatusWorking:
		frame := now().UnixMilli() / spinInterval.Milliseconds() % int64(len(spinner))
		return spinner[frame], uv.Style{Fg: t.Accent, Attrs: uv.AttrBold}
	case protocol.StatusWaiting:
		return "●", uv.Style{Fg: t.Attn, Attrs: uv.AttrBold}
	}
	return "○", uv.Style{Fg: t.Faint}
}
