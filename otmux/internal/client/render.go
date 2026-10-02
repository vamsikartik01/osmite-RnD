package client

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/mattn/go-runewidth"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/config"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/keys"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
)

func (c *Client) render() {
	c.mu.Lock()
	defer c.mu.Unlock()

	scr := c.term.Screen()
	if scr.Width() != c.cols || scr.Height() != c.rows {
		scr.Resize(c.cols, c.rows)
	}
	paneRows := max(c.rows-1, 0)
	layout := c.layout()

	// Everything is redrawn into the buffer each frame; the renderer only
	// emits the cells that actually changed.
	pt := c.paneTheme()
	blank := uv.Cell{Content: " ", Width: 1}
	if pt != nil {
		blank.Style.Bg = pt.Bg
	}
	for y := c.oy(); y < paneRows; y++ {
		for x := 0; x < c.cols; x++ {
			scr.SetCell(x, y, &blank)
		}
	}

	c.hits = nil // fresh slice: mouse handlers may hold the previous one
	switch layout {
	case config.LayoutSidebar:
		c.drawSidebar(scr, paneRows)
	case config.LayoutTwoBars:
		c.drawTopBar(scr)
	}

	// Panes are laid out by the daemon from x = 0; shift them past the sidebar.
	view := c.shifted()
	cursorX, cursorY, showCursor := 0, 0, false
	for _, p := range view.Panes {
		if h := c.history[p.ID]; h != nil {
			drawMirror(scr, h.mirror, p, pt)
			c.drawHistoryBadge(scr, p, h)
		} else {
			m := c.mirrors[p.ID]
			drawMirror(scr, m, p, pt)
			if p.Fresh {
				var bg color.Color
				if pt != nil {
					bg = pt.Bg
				}
				c.drawSplash(scr, p, m, bg)
			}
			if p.ID == c.state.ActivePane && m != nil {
				pos := m.emu.CursorPosition()
				if pos.X < p.W && pos.Y < p.H {
					cursorX, cursorY, showCursor = p.X+pos.X, p.Y+pos.Y, !m.cursorHidden
				}
			}
		}
		if c.sel != nil && c.sel.pane == p.ID {
			c.sel.highlight(scr, p.X, p.Y, p.W, p.H)
		}
	}
	c.drawDividers(scr, view)
	c.drawStatus(scr, c.rows-1)
	if c.overlay != nil {
		cursorX, cursorY = c.drawOverlay(scr, c.overlay)
		showCursor = true
	}
	if c.settings != nil {
		x, y := c.drawSettings(scr, c.settings)
		cursorX, cursorY, showCursor = x, y, x >= 0
	}

	if showCursor {
		scr.SetCursorPosition(cursorX, cursorY)
		scr.ShowCursor()
	} else {
		scr.HideCursor()
	}
	scr.Render()
	_ = scr.Flush()
	if showCursor {
		// ultraviolet's Flush queues the move to the cursor position inside
		// its renderer without sending it, so the cursor would stay where
		// the last cell was drawn until the next frame (e.g. one cell off
		// after typing). A second pass with nothing new to draw sends it.
		scr.Render()
		_ = scr.Flush()
	}
}

// paneTheme returns the theme to colour panes with, or nil to leave them in
// the terminal's own colours. Called with c.mu held.
func (c *Client) paneTheme() *Theme {
	if c.cfg.TerminalColors {
		return nil
	}
	return &c.theme
}

// shifted returns the state with panes and dividers moved right past the
// sidebar, in screen coordinates. Called with c.mu held.
func (c *Client) shifted() protocol.State {
	st := c.state
	ox, oy := c.ox(), c.oy()
	st.Panes = append([]protocol.PaneInfo(nil), st.Panes...)
	for i := range st.Panes {
		st.Panes[i].X += ox
		st.Panes[i].Y += oy
	}
	st.Dividers = append([]protocol.Divider(nil), st.Dividers...)
	for i := range st.Dividers {
		st.Dividers[i].X += ox
		st.Dividers[i].Y += oy
	}
	return st
}

// drawHistoryBadge marks a pane that shows scrollback rather than the live
// screen, in its top-right corner.
func (c *Client) drawHistoryBadge(scr uv.Screen, p protocol.PaneInfo, h *histView) {
	label := fmt.Sprintf(" ↑ %d of %d lines · type to return ", h.offset, h.total)
	if w := runewidth.StringWidth(label); w < p.W {
		put(scr, p.X+p.W-w, p.Y, label, uv.Style{Fg: c.theme.OnAccent, Bg: c.theme.Accent, Attrs: uv.AttrBold})
	}
}

// drawMirror copies a pane's mirror into its rectangle, clipping anything
// that doesn't fit (the mirror can briefly be larger during a resize).
//
// With a theme, default and ANSI colours are recoloured to match it.
func drawMirror(scr uv.Screen, m *mirror, p protocol.PaneInfo, th *Theme) {
	if m == nil {
		return
	}
	w, h := min(p.W, m.emu.Width()), min(p.H, m.emu.Height())
	for y := 0; y < h; y++ {
		for x := 0; x < w; {
			cell := m.emu.CellAt(x, y)
			if cell == nil {
				x++
				continue
			}
			cw := max(cell.Width, 1)
			if cell.Width == 0 && cell.Content == "" {
				x++ // second half of a wide character
				continue
			}
			if x+cw > w {
				break
			}
			if th != nil {
				cell = th.paneCell(cell)
			}
			scr.SetCell(p.X+x, p.Y+y, cell)
			x += cw
		}
	}
}

// drawDividers draws the lines between panes, choosing box-drawing glyphs
// from each cell's neighbours so junctions come out as ├ ┤ ┬ ┴ ┼. Segments
// around the focused pane are drawn in the accent colour.
func (c *Client) drawDividers(scr uv.Screen, st protocol.State) {
	if len(st.Dividers) == 0 {
		return
	}
	type pt struct{ x, y int }
	mask := map[pt]bool{}
	for _, d := range st.Dividers {
		for i := 0; i < d.Len; i++ {
			if d.Vertical {
				mask[pt{d.X, d.Y + i}] = true
			} else {
				mask[pt{d.X + i, d.Y}] = true
			}
		}
	}
	var active protocol.PaneInfo
	for _, p := range st.Panes {
		if p.ID == st.ActivePane {
			active = p
		}
	}
	t := c.theme
	for p := range mask {
		up, down := mask[pt{p.x, p.y - 1}], mask[pt{p.x, p.y + 1}]
		left, right := mask[pt{p.x - 1, p.y}], mask[pt{p.x + 1, p.y}]
		glyph := boxGlyph(up, down, left, right)
		fg := t.Border
		if p.x >= active.X-1 && p.x <= active.X+active.W && p.y >= active.Y-1 && p.y <= active.Y+active.H {
			fg = t.Accent
		}
		st := uv.Style{Fg: fg}
		if pt := c.paneTheme(); pt != nil {
			st.Bg = pt.Bg
		}
		scr.SetCell(p.x, p.y, &uv.Cell{Content: glyph, Width: 1, Style: st})
	}
}

func boxGlyph(up, down, left, right bool) string {
	vert, horiz := up || down, left || right
	switch {
	case up && down && left && right:
		return "┼"
	case up && down && right:
		return "├"
	case up && down && left:
		return "┤"
	case left && right && down:
		return "┬"
	case left && right && up:
		return "┴"
	case vert && !horiz:
		return "│"
	case horiz && !vert:
		return "─"
	case down && right:
		return "┌"
	case down && left:
		return "┐"
	case up && right:
		return "└"
	case up && left:
		return "┘"
	}
	return "│"
}

// drawStatus draws the bottom bar:
//
//	◆ api   web   infra │ 0 brewing  1 pondering ·2  +      Ctrl+B for shortcuts   14:32
//
// Every workspace is listed (click to switch), then the current workspace's
// tabs (click to switch, + for a new one). While the prefix is pending the
// whole bar turns into a key guide.
func (c *Client) drawStatus(s uv.Screen, y int) {
	if y < 0 {
		return
	}
	t := c.theme
	bar := uv.Style{Bg: t.Bar}
	fill(s, 0, y, c.cols, bar)
	key := uv.Style{Fg: t.Text, Bg: t.Bar, Attrs: uv.AttrBold}
	label := uv.Style{Fg: t.Muted, Bg: t.Bar}
	prefix := keys.Label(c.keys.Keymap().Prefix)

	if c.keys.Pending() {
		x := put(s, 1, y, " "+prefix+" ", uv.Style{Fg: hex(0x1c1c1e), Bg: t.Attn, Attrs: uv.AttrBold})
		x += 2
		for _, h := range keys.Hints {
			k := keys.Label(h.Key)
			if x+runewidth.StringWidth(k+" "+h.Label) >= c.cols {
				break
			}
			x = put(s, x, y, k, key)
			x = put(s, x, y, " "+h.Label+"   ", label)
		}
		return
	}

	x := 1
	switch c.layout() {
	case config.LayoutSidebar:
		// The sidebar lists the workspaces; here, the current one and its tabs.
		x0 := x
		x = put(s, x, y, " "+c.state.Workspace+" ", uv.Style{Fg: t.OnAccent, Bg: t.Accent, Attrs: uv.AttrBold})
		c.hits = append(c.hits, hit{x0, x, y, keys.Action{Name: keys.ActionChooseWorkspace}})
		x = put(s, x, y, " │ ", uv.Style{Fg: t.Faint, Bg: t.Bar})
		x = c.drawTabs(s, x, y)
	case config.LayoutTwoBars:
		// Tabs are in the top bar; this one lists the workspaces.
		x = c.drawWorkspaces(s, x, y, c.cols/2)
		x0 := x + 1
		x = put(s, x0, y, " + ", uv.Style{Fg: t.Muted, Bg: t.Bar})
		c.hits = append(c.hits, hit{x0, x, y, keys.Action{Name: keys.ActionPromptNewWorkspace}})
		x = c.drawPinnedChips(s, x, y, c.cols-45)
	default:
		x = c.drawWorkspaces(s, x, y, c.cols/2)
		x = put(s, x, y, " │ ", uv.Style{Fg: t.Faint, Bg: t.Bar})
		x = c.drawTabs(s, x, y)
	}
	c.drawRight(s, x, y, c.layout() != config.LayoutTwoBars)
}

// drawPinnedChips lists pinned tabs inline, for layouts without a sidebar.
// It stops before column limit.
func (c *Client) drawPinnedChips(s uv.Screen, x, y, limit int) int {
	t := c.theme
	if len(c.state.Pinned) == 0 {
		return x
	}
	x = put(s, x, y, " │ ", uv.Style{Fg: t.Faint, Bg: t.Bar})
	for _, p := range c.state.Pinned {
		if x+runewidth.StringWidth(p.Name)+4 > limit {
			break
		}
		x0 := x
		glyph, gst := statusGlyph(t, p.Status)
		gst.Bg = t.Bar
		x = put(s, x, y, " ", uv.Style{Bg: t.Bar})
		x = put(s, x, y, glyph, gst)
		x = put(s, x, y, " "+p.Name+" ", uv.Style{Fg: t.Muted, Bg: t.Bar})
		c.hits = append(c.hits, hit{x0, x, y, keys.Action{Name: protocol.ActionGotoTab, Arg: fmt.Sprint(p.TabID)}})
	}
	return x
}

// drawTopBar draws the tab bar used by the two-bars layout.
func (c *Client) drawTopBar(s uv.Screen) {
	t := c.theme
	fill(s, 0, 0, c.cols, uv.Style{Bg: t.Bar})
	x := c.drawTabs(s, 1, 0)
	if c.state.Zoomed && x+8 < c.cols {
		put(s, c.cols-8, 0, " ZOOM ", uv.Style{Fg: hex(0x1c1c1e), Bg: t.Attn, Attrs: uv.AttrBold})
	}
}

// drawTabs draws the current workspace's tabs and a + button, returning the
// column after them.
func (c *Client) drawTabs(s uv.Screen, x, y int) int {
	t := c.theme
	for i, tab := range c.state.Tabs {
		x0 := x
		active := i == c.state.Active
		bg, num, name := t.Bar, uv.Style{Fg: t.Faint, Bg: t.Bar}, uv.Style{Fg: t.Muted, Bg: t.Bar}
		if active {
			bg = t.Raised
			num = uv.Style{Fg: t.Accent, Bg: bg, Attrs: uv.AttrBold}
			name = uv.Style{Fg: t.Text, Bg: bg, Attrs: uv.AttrBold}
		}
		x = put(s, x, y, " ", uv.Style{Bg: bg})
		x = put(s, x, y, fmt.Sprint(i), num)
		if tab.Pinned {
			glyph, gst := statusGlyph(t, tab.Status)
			gst.Bg = bg
			x = put(s, x, y, " ", uv.Style{Bg: bg})
			x = put(s, x, y, glyph, gst)
		}
		x = put(s, x, y, " "+tab.Name, name)
		if tab.Panes > 1 {
			x = put(s, x, y, " ·"+fmt.Sprint(tab.Panes), uv.Style{Fg: t.Faint, Bg: bg})
		}
		x = put(s, x, y, " ", uv.Style{Bg: bg})
		c.hits = append(c.hits, hit{x0, x, y, keys.Action{Name: protocol.ActionSelectTab, Arg: fmt.Sprint(i)}})
		x++
	}
	x0 := x
	x = put(s, x, y, " + ", uv.Style{Fg: t.Muted, Bg: t.Bar})
	c.hits = append(c.hits, hit{x0, x, y, keys.Action{Name: keys.ActionPromptNewTab}})
	return x
}

// drawRight draws the right end of the bottom bar: zoom badge (unless the
// top bar shows it), detach button, prefix hint and clock. Segments that
// don't fit after column x are dropped, the clock last.
func (c *Client) drawRight(s uv.Screen, x, y int, showZoom bool) {
	t := c.theme
	bar := uv.Style{Bg: t.Bar}
	key := uv.Style{Fg: t.Text, Bg: t.Bar, Attrs: uv.AttrBold}
	label := uv.Style{Fg: t.Muted, Bg: t.Bar}
	prefix := keys.Label(c.keys.Keymap().Prefix)
	// Right side, built as segments and placed only if they fit.
	type seg struct {
		text  string
		style uv.Style
		act   *keys.Action // clickable
	}
	var right []seg
	if showZoom && c.state.Zoomed {
		right = append(right, seg{text: " ZOOM ", style: uv.Style{Fg: hex(0x1c1c1e), Bg: t.Attn, Attrs: uv.AttrBold}}, seg{text: "   ", style: bar})
	}
	right = append(right, seg{text: " detach ", style: uv.Style{Fg: t.Text, Bg: t.Raised}, act: &keys.Action{Name: keys.ActionDetach}}, seg{text: "   ", style: bar})
	right = append(right, seg{text: prefix, style: key}, seg{text: " for shortcuts   ", style: label})
	right = append(right, seg{text: time.Now().Format("15:04") + " ", style: uv.Style{Fg: t.Text, Bg: t.Bar}})
	// Drop segments from the front until the right side fits beside the tabs;
	// the clock goes last.
	for len(right) > 0 {
		w := 0
		for _, sg := range right {
			w += runewidth.StringWidth(sg.text)
		}
		if c.cols-w-1 > x+1 {
			rx := c.cols - w - 1
			for _, sg := range right {
				x0 := rx
				rx = put(s, rx, y, sg.text, sg.style)
				if sg.act != nil {
					c.hits = append(c.hits, hit{x0, rx, y, *sg.act})
				}
			}
			break
		}
		right = right[1:]
	}
}

// drawWorkspaces lists every workspace: the current one as a blue chip, the
// others as plain names. If they take too much room, only the current one
// is shown, with a count that opens the picker.
func (c *Client) drawWorkspaces(s uv.Screen, x, y, maxWidth int) int {
	t := c.theme
	chip := uv.Style{Fg: t.OnAccent, Bg: t.Accent, Attrs: uv.AttrBold}
	other := uv.Style{Fg: t.Muted, Bg: t.Bar}
	width := 0
	for _, w := range c.state.Workspaces {
		width += runewidth.StringWidth(w.Name) + 5
	}
	if width > maxWidth {
		x0 := x
		x = put(s, x, y, " "+c.state.Workspace+" ", chip)
		if n := len(c.state.Workspaces) - 1; n > 0 {
			x = put(s, x, y, fmt.Sprintf(" +%d ", n), other)
		}
		c.hits = append(c.hits, hit{x0, x, y, keys.Action{Name: keys.ActionChooseWorkspace}})
		return x
	}
	for i, w := range c.state.Workspaces {
		if i > 0 {
			x = put(s, x, y, " ", uv.Style{Bg: t.Bar})
		}
		x0 := x
		if w.Name == c.state.Workspace {
			x = put(s, x, y, " "+w.Name+" ", chip)
			c.hits = append(c.hits, hit{x0, x, y, keys.Action{Name: keys.ActionChooseWorkspace}})
		} else {
			x = put(s, x, y, " "+w.Name+" ", other)
			c.hits = append(c.hits, hit{x0, x, y, keys.Action{Name: protocol.ActionSwitchWorkspace, Arg: w.Name}})
		}
	}
	return x
}

// drawOverlay draws a floating panel and returns where the cursor goes.
//
//	╭─ Commands ──────────────────────────────╮
//	│ › split                                 │
//	│─────────────────────────────────────────│
//	│  Pane   Split right            Ctrl+B % │
//	│  Pane   Split down             Ctrl+B " │
//	│                                         │
//	│  ↑↓ select   enter run   esc close      │
//	╰─────────────────────────────────────────╯
func (c *Client) drawOverlay(s uv.Screen, o *overlay) (int, int) {
	t := c.theme
	panel := uv.Style{Fg: t.Text, Bg: t.Surface}
	frame := uv.Style{Fg: t.Raised, Bg: t.Surface}

	w := min(64, c.cols-4)
	listRows := 0
	if !o.prompt {
		listRows = min(max(len(o.view), 1), 12, max(c.rows-12, 1))
	}
	h := 5 // top border, input, spacer, footer, bottom border
	if !o.prompt {
		h += 1 + listRows // separator + rows
	}
	h = min(h, c.rows-1)
	if w < 20 || h < 4 {
		return 0, 0
	}
	bx := (c.cols - w) / 2
	by := max((c.rows-1-h)/4, 0)
	o.box = box{bx, by, w, h}

	for yy := by; yy < by+h; yy++ {
		fill(s, bx, yy, w, panel)
	}
	// Border with the title set into it.
	put(s, bx, by, "╭"+strings.Repeat("─", w-2)+"╮", frame)
	put(s, bx, by+h-1, "╰"+strings.Repeat("─", w-2)+"╯", frame)
	for yy := by + 1; yy < by+h-1; yy++ {
		put(s, bx, yy, "│", frame)
		put(s, bx+w-1, yy, "│", frame)
	}
	put(s, bx+2, by, " "+o.title+" ", uv.Style{Fg: t.Text, Bg: t.Surface, Attrs: uv.AttrBold})

	// Input line.
	iy := by + 1
	ix := put(s, bx+2, iy, "› ", uv.Style{Fg: t.Accent, Bg: t.Surface, Attrs: uv.AttrBold})
	cursorX := put(s, ix, iy, clipLeft(string(o.query), w-8), panel)
	if len(o.query) == 0 && o.placeholder != "" {
		put(s, ix, iy, o.placeholder, uv.Style{Fg: t.Faint, Bg: t.Surface})
	}

	footer := "enter save   esc cancel"
	if !o.prompt {
		footer = "↑↓ select   enter run   esc close"
		if o.footer != "" {
			footer = o.footer
		}
		put(s, bx+1, iy+1, strings.Repeat("─", w-2), frame)
		o.rowsY, o.visible = iy+2, listRows
		if o.sel < o.scroll {
			o.scroll = o.sel
		}
		if o.sel >= o.scroll+listRows {
			o.scroll = o.sel - listRows + 1
		}
		if len(o.view) == 0 {
			put(s, bx+3, o.rowsY, "No matches", uv.Style{Fg: t.Faint, Bg: t.Surface})
		}
		for r := 0; r < listRows && o.scroll+r < len(o.view); r++ {
			c.drawItem(s, o.view[o.scroll+r], bx+1, o.rowsY+r, w-2, o.scroll+r == o.sel)
		}
	}
	put(s, bx+3, by+h-2, footer, uv.Style{Fg: t.Faint, Bg: t.Surface})
	return cursorX, iy
}

func (c *Client) drawItem(s uv.Screen, it item, x, y, w int, selected bool) {
	t := c.theme
	bg, fg, dim, hintFg := t.Surface, t.Text, t.Faint, t.Muted
	if selected {
		bg, fg, dim, hintFg = t.Accent, t.OnAccent, t.OnAccent, t.OnAccent
	}
	fill(s, x, y, w, uv.Style{Bg: bg})
	cx := x + 2
	if it.group != "" {
		put(s, cx, y, it.group, uv.Style{Fg: dim, Bg: bg})
		cx += 11
	}
	label := it.label
	if it.current {
		label += "  ●"
	}
	hintW := runewidth.StringWidth(it.hint)
	put(s, cx, y, runewidth.Truncate(label, max(x+w-cx-hintW-3, 1), "…"), uv.Style{Fg: fg, Bg: bg})
	if it.hint != "" {
		put(s, x+w-hintW-2, y, it.hint, uv.Style{Fg: hintFg, Bg: bg})
	}
}

// clipLeft keeps the end of s visible within width cells.
func clipLeft(s string, width int) string {
	for runewidth.StringWidth(s) > width && s != "" {
		_, size := firstRune(s)
		s = s[size:]
	}
	return s
}

func firstRune(s string) (rune, int) {
	for i, r := range s {
		if i > 0 {
			return r, i
		}
	}
	return 0, len(s)
}

func fill(s uv.Screen, x, y, n int, st uv.Style) {
	for i := x; i < x+n; i++ {
		s.SetCell(i, y, &uv.Cell{Content: " ", Width: 1, Style: st})
	}
}

// put draws str at (x, y) and returns the column after it. Text past the
// right edge is dropped.
func put(s uv.Screen, x, y int, str string, st uv.Style) int {
	maxX := s.Bounds().Max.X
	for _, r := range str {
		rw := runewidth.RuneWidth(r)
		if rw == 0 {
			continue
		}
		if x+rw > maxX {
			break
		}
		if x >= 0 {
			s.SetCell(x, y, &uv.Cell{Content: string(r), Width: rw, Style: st})
		}
		x += rw
	}
	return x
}
