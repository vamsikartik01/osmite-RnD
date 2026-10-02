package client

import (
	"fmt"
	"strings"
	"unicode"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/keys"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
)

// overlay is a floating panel: either a filterable list (command palette,
// workspace picker) or a single text prompt. All fields are guarded by
// Client.mu.
type overlay struct {
	title       string
	placeholder string
	query       []rune

	// List mode.
	items  []item
	view   []item // items matching query, plus any "create" entry
	sel    int
	scroll int
	// If set, typing a name that matches nothing offers to run this action
	// with the typed text, e.g. create a workspace.
	createAction, createLabel string

	footer string // overrides the default key hint line

	// Prompt mode: Enter runs submit with the typed text as its argument.
	prompt bool
	submit string

	// Geometry from the last render, for mouse hits.
	box            box
	rowsY, visible int
}

type box struct{ x, y, w, h int }

func (b box) contains(x, y int) bool {
	return x >= b.x && x < b.x+b.w && y >= b.y && y < b.y+b.h
}

type item struct {
	group, label, hint string
	action             keys.Action
	current            bool   // marks e.g. the current workspace
	saved              string // name of the saved workspace this item opens
}

func promptOverlay(title, value, submit string) *overlay {
	return &overlay{title: title, query: []rune(value), prompt: true, submit: submit}
}

// paletteOverlay lists every action with its key, plus tabs and workspaces
// to jump to: the place to discover everything otmux can do.
func (c *Client) paletteOverlay() *overlay {
	km := c.keys.Keymap()
	prefix := keys.Label(km.Prefix)
	o := &overlay{title: "Commands", placeholder: "Type a command, tab or workspace"}
	for _, cmd := range keys.Catalog {
		hint := ""
		if k := km.KeyFor(cmd.Action); k != "" {
			hint = prefix + "  " + keys.Label(k)
		}
		o.items = append(o.items, item{group: cmd.Group, label: cmd.Title, hint: hint, action: cmd.Action})
	}
	c.mu.Lock()
	st := c.state
	c.mu.Unlock()
	for _, p := range st.Pinned {
		o.items = append(o.items, item{
			group: "Watch", label: p.Name, hint: p.Workspace,
			action: keys.Action{Name: protocol.ActionGotoTab, Arg: fmt.Sprint(p.TabID)},
		})
	}
	for i, t := range st.Tabs {
		hint := ""
		if i < 10 {
			hint = fmt.Sprintf("%s  %d", prefix, i)
		}
		o.items = append(o.items, item{
			group: "Go to", label: fmt.Sprintf("Tab %d · %s", i, t.Name), hint: hint,
			action: keys.Action{Name: protocol.ActionSelectTab, Arg: fmt.Sprint(i)}, current: i == st.Active,
		})
	}
	c.mu.Lock()
	for _, it := range c.savedItems(st) {
		it.group, it.label = "Open", "Workspace · "+it.label
		o.items = append(o.items, it)
	}
	c.mu.Unlock()
	for _, w := range st.Workspaces {
		o.items = append(o.items, item{
			group: "Go to", label: "Workspace · " + w.Name,
			action:  keys.Action{Name: protocol.ActionSwitchWorkspace, Arg: w.Name},
			current: w.Name == st.Workspace,
		})
	}
	o.refilter()
	return o
}

// workspaceOverlay lists workspaces; typing a new name creates one.
func (c *Client) workspaceOverlay() *overlay {
	c.mu.Lock()
	st := c.state
	c.mu.Unlock()
	o := &overlay{
		title: "Workspaces", placeholder: "Filter, or type a new name",
		createAction: protocol.ActionNewWorkspace, createLabel: "Create workspace",
		footer: "↑↓ select   enter open   ctrl+d remove saved   esc close",
	}
	for _, w := range st.Workspaces {
		hint := plural(w.Tabs, "tab")
		if w.Clients > 0 {
			hint += " · " + plural(w.Clients, "client")
		}
		o.items = append(o.items, item{
			label: w.Name, hint: hint, current: w.Name == st.Workspace,
			action: keys.Action{Name: protocol.ActionSwitchWorkspace, Arg: w.Name},
		})
	}
	c.mu.Lock()
	for _, it := range c.savedItems(st) {
		o.items = append(o.items, it)
	}
	c.mu.Unlock()
	o.refilter()
	for i, it := range o.view {
		if it.current {
			o.sel = i
		}
	}
	return o
}

// savedItems lists saved workspaces that aren't running. Opening one starts
// it in its folder. Called with c.mu held.
func (c *Client) savedItems(st protocol.State) []item {
	running := map[string]bool{}
	for _, w := range st.Workspaces {
		running[strings.ToLower(w.Name)] = true
	}
	var out []item
	for _, p := range c.cfg.Workspaces {
		if running[strings.ToLower(p.Name)] {
			continue
		}
		out = append(out, item{
			label: p.Name, hint: "saved · " + shortPath(p.Path), saved: p.Name,
			action: keys.Action{Name: protocol.ActionNewWorkspace, Arg: p.Name, Dir: p.Path},
		})
	}
	return out
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// refilter recomputes the visible items for the current query.
func (o *overlay) refilter() {
	q := strings.ToLower(strings.TrimSpace(string(o.query)))
	o.view = o.view[:0]
	exact := false
	for _, it := range o.items {
		if fuzzy(strings.ToLower(it.group+" "+it.label), q) {
			o.view = append(o.view, it)
		}
		if strings.EqualFold(it.label, q) {
			exact = true
		}
	}
	if o.createAction != "" && q != "" && !exact {
		name := strings.TrimSpace(string(o.query))
		o.view = append(o.view, item{
			label: fmt.Sprintf("%s “%s”", o.createLabel, name), hint: "enter",
			action: keys.Action{Name: o.createAction, Arg: name},
		})
	}
	o.sel = min(max(o.sel, 0), max(len(o.view)-1, 0))
}

// fuzzy reports whether every rune of pattern appears in s, in order.
func fuzzy(s, pattern string) bool {
	if pattern == "" {
		return true
	}
	pr := []rune(pattern)
	i := 0
	for _, r := range s {
		if r == pr[i] {
			i++
			if i == len(pr) {
				return true
			}
		}
	}
	return false
}

func (o *overlay) insert(text string) {
	for _, r := range text {
		if unicode.IsPrint(r) {
			o.query = append(o.query, r)
		}
	}
	if !o.prompt {
		o.sel = 0
		o.refilter()
	}
}

// choose returns the action Enter would run.
func (o *overlay) choose() (keys.Action, bool) {
	if o.prompt {
		v := strings.TrimSpace(string(o.query))
		if v == "" && o.submit != protocol.ActionNewWorkspace && o.submit != protocol.ActionNewTab {
			return keys.Action{}, false
		}
		return keys.Action{Name: o.submit, Arg: v}, true
	}
	if o.sel >= 0 && o.sel < len(o.view) {
		return o.view[o.sel].action, true
	}
	return keys.Action{}, false
}

// overlayKey handles a key while an overlay is open. It returns an action to
// run when the user picked something; the overlay is closed by then.
func (c *Client) overlayKey(k uv.KeyPressEvent) (keys.Action, bool) {
	if keys.IsModifierOnly(k) {
		return keys.Action{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	o := c.overlay
	switch {
	case k.MatchString("esc", "ctrl+c", "ctrl+g"):
		c.overlay = nil
	case k.MatchString("enter"):
		act, ok := o.choose()
		c.overlay = nil
		return act, ok
	case k.MatchString("up", "ctrl+p", "shift+tab"):
		if len(o.view) > 0 {
			o.sel = (o.sel - 1 + len(o.view)) % len(o.view)
		}
	case k.MatchString("down", "ctrl+n", "tab"):
		if len(o.view) > 0 {
			o.sel = (o.sel + 1) % len(o.view)
		}
	case k.MatchString("backspace"):
		if n := len(o.query); n > 0 {
			o.query = o.query[:n-1]
			if !o.prompt {
				o.refilter()
			}
		}
	case k.MatchString("ctrl+d"):
		// Remove the selected saved workspace from the list.
		if o.sel >= 0 && o.sel < len(o.view) && o.view[o.sel].saved != "" {
			name := o.view[o.sel].saved
			c.cfg.RemoveProfile(name)
			_ = c.cfg.Save()
			kept := o.items[:0]
			for _, it := range o.items {
				if it.saved != name {
					kept = append(kept, it)
				}
			}
			o.items = kept
			o.refilter()
		}
	case k.MatchString("ctrl+u"):
		o.query = o.query[:0]
		if !o.prompt {
			o.refilter()
		}
	case k.Text != "":
		o.insert(k.Text)
	}
	return keys.Action{}, false
}

// click handles a mouse click while the overlay is open: on an item it picks
// it, outside the panel it closes the overlay.
func (o *overlay) click(x, y int) (act keys.Action, ok, closed bool) {
	if !o.box.contains(x, y) {
		return keys.Action{}, false, true
	}
	if o.prompt {
		return keys.Action{}, false, false
	}
	row := y - o.rowsY
	if row >= 0 && row < o.visible && o.scroll+row < len(o.view) {
		o.sel = o.scroll + row
		act, ok = o.choose()
		return act, ok, false
	}
	return keys.Action{}, false, false
}

func (c *Client) closeOverlay() {
	c.mu.Lock()
	c.overlay = nil
	c.mu.Unlock()
}
