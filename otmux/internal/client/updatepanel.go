package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/mattn/go-runewidth"

	"github.com/vamsikartik01/osmite-RnD/otmux"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/keys"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/update"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/version"
)

// ErrRestart is what Attach returns when the user chose to restart otmux
// from the update panel: the caller runs the installed program as
// `otmux restart -y`, which stops the daemon and opens otmux again.
var ErrRestart = errors.New("restart otmux")

// Client-only actions for the update panel.
const (
	actionUpdatePanel = "update-panel"
	actionRestartNow  = "restart-otmux"
)

// updatePanel is the popup the update badge opens: what changed in the
// installed version, and a button to restart into it (asking first, since
// a restart closes every shell). All fields are guarded by Client.mu.
type updatePanel struct {
	target  string           // the installed version
	notes   []update.Release // what changed, once loaded
	loading bool
	failed  string // why the notes couldn't be read
	confirm bool   // showing "close N tabs and restart?"
	sel     int    // focused button: 0 the first, 1 the second
	scroll  int    // first notes line shown

	// Geometry from the last render, for mouse hits.
	box     box
	buttons [2]box
	notesH  int // notes lines that fit
	lines   int // notes lines in all
}

// openUpdatePanel shows the update panel, if an update is waiting for a
// restart, and starts reading what changed. Called with c.mu held.
func (c *Client) openUpdatePanel() {
	if c.updateTarget == "" {
		return
	}
	c.overlay, c.settings = nil, nil
	p := &updatePanel{target: c.updateTarget, loading: true}
	c.upd = p
	since, self := c.updateSince, c.updateTarget == version.Version
	go func() {
		notes, err := readNotes(since, self)
		c.mu.Lock()
		p.loading, p.notes = false, notes
		if err != nil {
			p.failed = err.Error()
		}
		c.mu.Unlock()
		c.markDirty()
	}()
}

// readNotes returns what changed after version since. When this program is
// the new version (the daemon is the old one) the notes are built in;
// otherwise they come from the newly installed program file.
func readNotes(since string, self bool) ([]update.Release, error) {
	if self {
		return update.Notes(otmux.Changelog, since, version.Version), nil
	}
	exe, err := update.Executable()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, exe, "changes", since).Output()
	if err != nil {
		return nil, fmt.Errorf("couldn't read them from the new version: %w", err)
	}
	var notes []update.Release
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "- ") && len(notes) > 0:
			notes[len(notes)-1].Items = append(notes[len(notes)-1].Items, line[2:])
		case line != "":
			notes = append(notes, update.Release{Version: line})
		}
	}
	return notes, nil
}

// buttonLabels are the panel's two buttons, before and after Restart now.
func (p *updatePanel) buttonLabels() [2]string {
	if p.confirm {
		return [2]string{"Yes, restart", "Cancel"}
	}
	return [2]string{"Restart now", "Later"}
}

// press runs button i. It returns an action for the client to run.
// Called with c.mu held.
func (c *Client) pressUpdateButton(i int) (keys.Action, bool) {
	p := c.upd
	switch {
	case !p.confirm && i == 0:
		p.confirm, p.sel = true, 1 // ask first; Cancel has the focus
	case !p.confirm:
		c.upd = nil
	case i == 0:
		c.upd = nil
		return keys.Action{Name: actionRestartNow}, true
	default:
		p.confirm, p.sel = false, 0
	}
	return keys.Action{}, false
}

// updatePanelKey handles a key while the panel is open.
func (c *Client) updatePanelKey(k uv.KeyPressEvent) (keys.Action, bool) {
	if keys.IsModifierOnly(k) {
		return keys.Action{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.upd
	switch {
	case k.MatchString("esc", "ctrl+c", "q"):
		if p.confirm {
			p.confirm, p.sel = false, 0
		} else {
			c.upd = nil
		}
	case k.MatchString("left", "right", "tab", "shift+tab", "h", "l"):
		p.sel = 1 - p.sel
	case k.MatchString("up", "k"):
		p.scroll = max(p.scroll-1, 0)
	case k.MatchString("down", "j"):
		p.scroll = min(p.scroll+1, max(p.lines-p.notesH, 0))
	case k.MatchString("enter", "space"):
		return c.pressUpdateButton(p.sel)
	}
	return keys.Action{}, false
}

// updatePanelMouse handles the mouse while the panel is open: buttons,
// the wheel over the notes, and a click outside closes it.
func (c *Client) updatePanelMouse(kind protocol.MouseKind, button uv.MouseButton, x, y int) (keys.Action, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.upd
	switch kind {
	case protocol.MouseWheel:
		if button == uv.MouseWheelUp {
			p.scroll = max(p.scroll-1, 0)
		} else if button == uv.MouseWheelDown {
			p.scroll = min(p.scroll+1, max(p.lines-p.notesH, 0))
		}
	case protocol.MouseClick:
		if !p.box.contains(x, y) {
			c.upd = nil
			return keys.Action{}, false
		}
		for i, b := range p.buttons {
			if b.contains(x, y) {
				p.sel = i
				return c.pressUpdateButton(i)
			}
		}
	}
	return keys.Action{}, false
}

// tabCount is how many tabs (and so shells) a restart would close.
// Called with c.mu held.
func (c *Client) tabCount() (tabs, workspaces int) {
	for _, w := range c.state.Workspaces {
		tabs += w.Tabs
	}
	return tabs, len(c.state.Workspaces)
}

// drawUpdatePanel draws the panel.
//
//	╭─ otmux 1.3.2 is installed ───────────────────────╮
//	│                                                  │
//	│  What's new                                      │
//	│  • An agent at its prompt shows as idle…         │
//	│                                                  │
//	│  Restarting closes 7 tabs in 2 workspaces and    │
//	│  everything running in them, agents included.    │
//	│                                                  │
//	│   Restart now    Later                           │
//	│                                                  │
//	│  From a terminal: otmux restart                  │
//	│  ←→ choose   enter press   esc close             │
//	╰──────────────────────────────────────────────────╯
func (c *Client) drawUpdatePanel(s uv.Screen, p *updatePanel) {
	t := c.theme
	panel := uv.Style{Fg: t.Text, Bg: t.Surface}
	muted := uv.Style{Fg: t.Muted, Bg: t.Surface}
	faint := uv.Style{Fg: t.Faint, Bg: t.Surface}
	bold := uv.Style{Fg: t.Text, Bg: t.Surface, Attrs: uv.AttrBold}
	frame := uv.Style{Fg: t.Raised, Bg: t.Surface}

	w := min(72, c.cols-4)
	if w < 36 || c.rows < 14 {
		return
	}
	cw := w - 6 // content width

	// The notes, wrapped: version headings when there's more than one.
	type line struct {
		text string
		st   uv.Style
	}
	var notes []line
	switch {
	case p.loading:
		notes = append(notes, line{"Reading what changed…", faint})
	case p.failed != "":
		notes = append(notes, line{"Couldn't show what changed: " + p.failed, faint})
	case len(p.notes) == 0:
		notes = append(notes, line{"Fixes and small improvements.", muted})
	}
	for _, r := range p.notes {
		if len(p.notes) > 1 {
			if len(notes) > 0 {
				notes = append(notes, line{"", panel})
			}
			notes = append(notes, line{r.Version, muted})
		}
		for _, it := range r.Items {
			for i, part := range wrap(it, cw-2) {
				lead := "  "
				if i == 0 {
					lead = "• "
				}
				notes = append(notes, line{lead + part, panel})
			}
		}
	}

	tabs, wss := c.tabCount()
	what := fmt.Sprintf("Restarting closes %s in %s and everything running in them, agents included.",
		plural(tabs, "tab"), plural(wss, "workspace"))
	whatStyle := muted
	if p.confirm {
		what = fmt.Sprintf("Close %s and restart otmux? This can't be undone.", plural(tabs, "tab"))
		whatStyle = uv.Style{Fg: t.Attn, Bg: t.Surface, Attrs: uv.AttrBold}
	}
	whatLines := wrap(what, cw)

	// Fixed rows: borders 2, blank + heading 2, blank + what, blank +
	// buttons 2, blank + terminal hint 2, key hints 1.
	fixed := 2 + 2 + 1 + len(whatLines) + 2 + 2 + 1
	maxH := c.rows - 2
	p.lines = len(notes)
	p.notesH = max(min(len(notes), maxH-fixed), 1)
	p.scroll = min(p.scroll, max(len(notes)-p.notesH, 0))
	h := fixed + p.notesH

	bx, by := (c.cols-w)/2, max((c.rows-1-h)/3, 0)
	p.box = box{bx, by, w, h}
	for yy := by; yy < by+h; yy++ {
		fill(s, bx, yy, w, panel)
	}
	put(s, bx, by, "╭"+strings.Repeat("─", w-2)+"╮", frame)
	put(s, bx, by+h-1, "╰"+strings.Repeat("─", w-2)+"╯", frame)
	for yy := by + 1; yy < by+h-1; yy++ {
		put(s, bx, yy, "│", frame)
		put(s, bx+w-1, yy, "│", frame)
	}
	put(s, bx+2, by, " otmux "+p.target+" is installed ", bold)

	x, y := bx+3, by+2
	put(s, x, y, "What's new", bold)
	if p.lines > p.notesH {
		more := fmt.Sprintf("%d–%d of %d  ↑↓", p.scroll+1, p.scroll+p.notesH, p.lines)
		put(s, bx+w-3-runewidth.StringWidth(more), y, more, faint)
	}
	y++
	for i := 0; i < p.notesH && p.scroll+i < len(notes); i++ {
		n := notes[p.scroll+i]
		put(s, x, y+i, runewidth.Truncate(n.text, cw, "…"), n.st)
	}
	y += p.notesH + 1
	for _, l := range whatLines {
		put(s, x, y, l, whatStyle)
		y++
	}
	y++

	// Buttons: the focused one stands out; Yes, restart in the warning colour.
	bxx := x
	for i, label := range p.buttonLabels() {
		st := uv.Style{Fg: t.Text, Bg: t.Raised}
		if i == p.sel {
			st = uv.Style{Fg: t.OnAccent, Bg: t.Accent, Attrs: uv.AttrBold}
			if p.confirm && i == 0 {
				st = uv.Style{Fg: AttnText, Bg: t.Attn, Attrs: uv.AttrBold}
			}
		}
		end := put(s, bxx, y, " "+label+" ", st)
		p.buttons[i] = box{bxx, y, end - bxx, 1}
		bxx = end + 2
	}
	y += 2
	hx := put(s, x, y, "From a terminal: ", faint)
	put(s, hx, y, "otmux restart", muted)
	drawKeyHints(s, x, by+h-2, "←→ choose   enter press   esc close", t, t.Surface)
}
