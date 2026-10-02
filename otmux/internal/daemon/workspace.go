package daemon

import (
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/layout"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
)

// statusRows is how many rows of the client's terminal the status bar uses.
const statusRows = 1

// Workspace is a named set of tabs that clients attach to. It outlives its
// clients: detaching leaves every shell running.
//
// All fields are guarded by Server.mu.
type Workspace struct {
	name    string
	dir     string // where new shells start unless a pane reports its own cwd
	tabs    []*Tab
	active  int
	lastTab uint32 // tab ID, for last-tab
	clients map[*client]struct{}

	// Size of the most recently resized client's terminal. With several
	// clients attached, the latest one wins (like tmux's "latest" option).
	cols, rows int
}

// Tab is a set of panes arranged by a split tree.
type Tab struct {
	id       uint32
	name     string // what the user called it, or a random friendly name
	tree     *layout.Tree
	panes    map[uint32]*Pane
	active   uint32 // focused pane
	lastPane uint32
	zoomed   bool

	// Pinning. A tab is pinned when the user pinned it, or when it runs a
	// coding agent and the user hasn't unpinned it.
	pin       *bool  // the user's choice; nil means automatic
	pinSeq    uint64 // order in the pinned list
	working   bool   // an agent is producing output by itself
	attention bool   // rang the bell or sent a notification while unwatched
}

// agent returns the coding agent running in any of the tab's panes.
func (t *Tab) agent() string {
	for _, id := range t.tree.Panes() {
		if p := t.panes[id]; p != nil && p.agent != "" {
			return p.agent
		}
	}
	return ""
}

func (t *Tab) pinned() bool {
	if t.pin != nil {
		return *t.pin
	}
	return t.agent() != ""
}

// status describes what the tab's agent is doing, whether or not anyone is
// looking: working while it produces output by itself, waiting when it's
// idle at its prompt. A bell or notification while unwatched also means
// waiting, even mid-spinner (e.g. a permission prompt).
func (t *Tab) status() string {
	switch {
	case t.attention:
		return protocol.StatusWaiting
	case t.working:
		return protocol.StatusWorking
	case t.agent() != "":
		return protocol.StatusWaiting
	}
	return protocol.StatusIdle
}

// watched reports whether a client is looking at tab t right now.
func (w *Workspace) watched(t *Tab) bool {
	return len(w.clients) > 0 && w.activeTab() == t
}

func (w *Workspace) area() layout.Rect {
	return layout.Rect{W: max(w.cols, 1), H: max(w.rows-statusRows, 1)}
}

func (w *Workspace) activeTab() *Tab {
	if len(w.tabs) == 0 {
		return nil
	}
	return w.tabs[w.active]
}

func (w *Workspace) tabIndex(t *Tab) int {
	for i, x := range w.tabs {
		if x == t {
			return i
		}
	}
	return -1
}

// view returns the rectangles of the panes that are on screen and the
// dividers between them. A zoomed tab shows only its active pane.
func (t *Tab) view(area layout.Rect) (map[uint32]layout.Rect, []layout.Divider) {
	if t.zoomed {
		return map[uint32]layout.Rect{t.active: area}, nil
	}
	return t.tree.Layout(area)
}

// relayout resizes every pane to its place in the layout.
func (t *Tab) relayout(area layout.Rect) {
	rects, _ := t.view(area)
	for id, r := range rects {
		if p := t.panes[id]; p != nil {
			p.resize(max(r.W, 1), max(r.H, 1))
		}
	}
}

func (t *Tab) activePane() *Pane { return t.panes[t.active] }

func (t *Tab) focus(id uint32) {
	if id == 0 || id == t.active || t.panes[id] == nil {
		return
	}
	t.lastPane, t.active = t.active, id
}

func (w *Workspace) info() protocol.WorkspaceInfo {
	return protocol.WorkspaceInfo{Name: w.name, Tabs: len(w.tabs), Clients: len(w.clients)}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
