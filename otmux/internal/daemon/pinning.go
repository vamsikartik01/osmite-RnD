package daemon

import (
	"sort"
	"strconv"
	"time"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/agents"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/platform"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
)

const (
	// agentScanInterval is how often the daemon looks for coding agents.
	agentScanInterval = time.Second
	// workingWindow: an agent counts as working while it keeps producing
	// output by itself, each burst this soon after the last (see workBursts).
	workingWindow = 1500 * time.Millisecond
)

// watchAgents periodically finds the coding agents running in panes, pins
// their tabs, and tracks whether each is working or waiting.
func (s *Server) watchAgents() {
	t := time.NewTicker(agentScanInterval)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			s.scanAgents()
		}
	}
}

func (s *Server) scanAgents() {
	s.mu.Lock()
	var roots []int
	for _, ws := range s.workspaces {
		for _, t := range ws.tabs {
			for _, p := range t.panes {
				if !p.exited {
					roots = append(roots, p.pty.Pid())
				}
			}
		}
	}
	s.mu.Unlock()

	// Listing processes can be slow; don't hold the lock for it.
	procs, err := platform.Processes(roots)
	agentOf := map[int]string{}
	if err == nil {
		for _, root := range roots {
			for _, d := range platform.Descendants(procs, root) {
				if a := agents.Detect(d.Args); a != "" {
					agentOf[root] = a
					break
				}
			}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	now := time.Now()
	for _, ws := range s.workspaces {
		for _, t := range ws.tabs {
			wasPinned, wasStatus := t.pinned(), t.status()
			recent := false
			for _, p := range t.panes {
				if p.exited {
					continue
				}
				if a := agentOf[p.pty.Pid()]; err == nil && a != p.agent {
					p.agent = a
					changed = true // pane titles show it
				}
				if p.agent != "" && p.working(now) {
					recent = true
				}
			}
			if ws.watched(t) {
				t.attention = false
			} else if t.working && !recent {
				t.attention = true // finished while nobody was looking: your turn
			}
			t.working = recent
			if t.pinned() && !wasPinned {
				s.pinSeq++
				t.pinSeq = s.pinSeq
			}
			if t.pinned() != wasPinned || t.status() != wasStatus {
				changed = true
			}
		}
	}
	if changed {
		s.broadcastStates()
	}
}

// pinnedList returns every pinned tab across workspaces, in pin order.
func (s *Server) pinnedList() []protocol.PinnedTab {
	type entry struct {
		seq uint64
		pt  protocol.PinnedTab
	}
	var es []entry
	for _, ws := range s.workspaces {
		for _, t := range ws.tabs {
			if t.pinned() {
				es = append(es, entry{t.pinSeq, protocol.PinnedTab{
					TabID: t.id, Name: t.name, Workspace: ws.name, Agent: t.agent(), Status: t.status(),
				}})
			}
		}
	}
	sort.Slice(es, func(i, j int) bool { return es[i].seq < es[j].seq })
	out := make([]protocol.PinnedTab, len(es))
	for i, e := range es {
		out[i] = e.pt
	}
	return out
}

// togglePin pins or unpins tab t by hand. Unpinning an agent's tab keeps it
// unpinned until the user pins it again.
func (s *Server) togglePin(t *Tab) {
	v := !t.pinned()
	t.pin = &v
	if v {
		s.pinSeq++
		t.pinSeq = s.pinSeq
	} else {
		t.attention = false
	}
	s.broadcastStates()
}

// noticeBell flags p's tab when its program rang the bell or sent a desktop
// notification while nobody was looking at it.
func (s *Server) noticeBell(p *Pane) {
	p.notified = false
	t := p.tab
	if p.ws.watched(t) || t.attention {
		return
	}
	t.attention = true
	if t.pinned() {
		s.broadcastStates()
	}
}

// findTab finds a tab by ID in any workspace.
func (s *Server) findTab(id uint32) (*Workspace, int) {
	for _, ws := range s.workspaces {
		for i, t := range ws.tabs {
			if t.id == id {
				return ws, i
			}
		}
	}
	return nil, -1
}

// gotoTab shows tab id to c, switching workspace if needed.
func (s *Server) gotoTab(c *client, id uint32) {
	ws, i := s.findTab(id)
	if ws == nil {
		return
	}
	ws.tabs[i].attention = false
	if ws == c.ws {
		s.selectTab(ws, i)
		s.broadcastView(ws)
		s.broadcastStates()
		return
	}
	if i != ws.active {
		ws.lastTab = ws.tabs[ws.active].id
		ws.active = i
	}
	s.join(c, ws)
}

// cyclePinned moves c to the next (step 1) or previous (step -1) pinned tab.
func (s *Server) cyclePinned(c *client, step int) {
	list := s.pinnedList()
	if len(list) == 0 {
		return
	}
	cur := -1
	if t := c.ws.activeTab(); t != nil {
		for i, p := range list {
			if p.TabID == t.id {
				cur = i
			}
		}
	}
	next := 0
	switch {
	case cur >= 0:
		next = (cur + step + len(list)) % len(list)
	case step < 0:
		next = len(list) - 1
	}
	s.gotoTab(c, list[next].TabID)
}

func parseID(s string) uint32 {
	id, _ := strconv.ParseUint(s, 10, 32)
	return uint32(id)
}
