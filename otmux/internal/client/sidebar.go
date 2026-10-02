package client

import (
	"fmt"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/mattn/go-runewidth"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/config"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/keys"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
)

// sidebarWidth is the sidebar's width in cells; one more blank column
// separates it from the panes.
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

// ox is how far right the panes start: past the sidebar if it's shown.
// Called with c.mu held.
func (c *Client) ox() int {
	if c.layout() == config.LayoutSidebar {
		return sidebarWidth + 1
	}
	return 0
}

// oy is how far down the panes start: below the top tab bar if it's shown.
// Called with c.mu held.
func (c *Client) oy() int {
	if c.layout() == config.LayoutTwoBars {
		return 1
	}
	return 0
}

// paneCols is the width the daemon lays panes out in. Called with c.mu held.
func (c *Client) paneCols() int { return max(c.cols-c.ox(), 1) }

// daemonRows is the height to report to the daemon, which keeps one row for
// the bottom bar itself. Called with c.mu held.
func (c *Client) daemonRows() int { return max(c.rows-c.oy(), 2) }

// drawSidebar draws the sidebar on one alignment grid: section headings at
// column 1, rows straight below them, counts right-aligned, and the current
// item in a full-width highlighted box.
//
//	 WATCH
//	 ● claude-refactor
//	 ◉ codex-tests
//	 ○ notes
//
//	 WORKSPACES            +
//	[  api                 3 ]   current
//	   web                 1
//	   lab             saved
//	…
//	   Settings     Ctrl+B s
func (c *Client) drawSidebar(s uv.Screen, h int) {
	t := c.theme
	w := sidebarWidth
	plain := uv.Style{Fg: t.Muted, Bg: t.Bar}
	faint := uv.Style{Fg: t.Faint, Bg: t.Bar}
	boxed := uv.Style{Fg: t.Text, Bg: t.Raised, Attrs: uv.AttrBold}
	for y := 0; y < h; y++ {
		fill(s, 0, y, w, uv.Style{Bg: t.Bar})
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

	// Watch list, capped at half the sidebar so workspaces stay visible.
	y := 1
	heading(y, "WATCH")
	y++
	watchEnd := max(y+(bottom-y)/2, y+3)
	var current uint32
	if i := c.state.Active; i >= 0 && i < len(c.state.Tabs) {
		current = c.state.Tabs[i].ID
	}
	for i, p := range c.state.Pinned {
		if y >= watchEnd-1 && i < len(c.state.Pinned)-1 {
			row(y, 3, faint, fmt.Sprintf("+%d more · %s Tab", len(c.state.Pinned)-i, prefix), "", nil)
			y++
			break
		}
		st := plain
		if p.TabID == current {
			st = boxed
		}
		row(y, 3, st, p.Name, "", &keys.Action{Name: protocol.ActionGotoTab, Arg: fmt.Sprint(p.TabID)})
		glyph, gst := statusGlyph(t, p.Status)
		gst.Bg = st.Bg
		put(s, 1, y, glyph, gst)
		y++
	}
	if len(c.state.Pinned) == 0 {
		row(y, 1, faint, "Agents show up here", "", nil)
		y++
	}

	// Workspaces, with + in the heading to add one.
	y++
	if y >= bottom-1 {
		return
	}
	heading(y, "WORKSPACES")
	put(s, w-2, y, "+", uv.Style{Fg: t.Muted, Bg: t.Bar, Attrs: uv.AttrBold})
	c.hits = append(c.hits, hit{x0: w - 4, x1: w, y: y, action: keys.Action{Name: keys.ActionPromptNewWorkspace}})
	y++
	running := map[string]bool{}
	for _, ws := range c.state.Workspaces {
		running[strings.ToLower(ws.Name)] = true
		if y >= bottom-1 {
			break
		}
		if ws.Name == c.state.Workspace {
			row(y, 3, boxed, ws.Name, fmt.Sprint(ws.Tabs), &keys.Action{Name: keys.ActionChooseWorkspace})
		} else {
			row(y, 3, plain, ws.Name, fmt.Sprint(ws.Tabs), &keys.Action{Name: protocol.ActionSwitchWorkspace, Arg: ws.Name})
		}
		y++
	}
	for _, p := range c.cfg.Workspaces {
		if running[strings.ToLower(p.Name)] || y >= bottom-1 {
			continue
		}
		row(y, 3, faint, p.Name, "saved", &keys.Action{Name: protocol.ActionNewWorkspace, Arg: p.Name, Dir: p.Path})
		y++
	}

	if bottom > y {
		row(bottom, 3, plain, "Settings", prefix+" s", &keys.Action{Name: keys.ActionSettings})
	}
}

// blinkInterval is how long a working dot stays in each phase.
const blinkInterval = 500 * time.Millisecond

// now is the clock used for blinking; tests replace it.
var now = time.Now

func blinkOn() bool { return now().UnixMilli()/blinkInterval.Milliseconds()%2 == 0 }

// anyWorking reports whether a watched tab's dot is pulsing, so the screen
// needs redrawing to animate it. Called with c.mu held.
func (c *Client) anyWorking() bool {
	for _, p := range c.state.Pinned {
		if p.Status == protocol.StatusWorking {
			return true
		}
	}
	return false
}

// statusGlyph is the dot shown for a watched tab: ● working (accent),
// ◉ waiting for you (attention), ○ idle.
func statusGlyph(t Theme, status string) (string, uv.Style) {
	switch status {
	case protocol.StatusWorking:
		// Pulse while the agent works: accent, then dim, twice a second.
		if blinkOn() {
			return "●", uv.Style{Fg: t.Accent, Attrs: uv.AttrBold}
		}
		return "●", uv.Style{Fg: t.Faint}
	case protocol.StatusWaiting:
		return "◉", uv.Style{Fg: t.Attn, Attrs: uv.AttrBold}
	}
	return "○", uv.Style{Fg: t.Faint}
}
