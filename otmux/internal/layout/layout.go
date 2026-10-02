// Package layout arranges a tab's panes as a binary split tree, the way tmux
// and Zellij do. It is pure geometry: no PTYs, no I/O. That keeps it easy to
// test, and lets the daemon and future clients share it.
package layout

import "math"

// Rect is a cell rectangle. W and H are sizes, not end coordinates.
type Rect struct{ X, Y, W, H int }

// Contains reports whether (x, y) is inside r.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Dir is the direction a split node lays out its two children.
type Dir int

const (
	// Row places children side by side, with a vertical divider between them
	// (tmux "split-window -h", prefix %).
	Row Dir = iota
	// Column stacks children, with a horizontal divider between them
	// (tmux "split-window -v", prefix ").
	Column
)

// Direction is used for focus moves and resizing.
type Direction int

const (
	Left Direction = iota
	Right
	Up
	Down
)

// Divider is the line between the two children of a split node.
type Divider struct {
	Node     uint32 // split node ID, used to drag the divider
	X, Y     int
	Len      int
	Vertical bool // true for a Row split's divider
}

// Node is either a leaf holding a pane, or a split with two children.
type Node struct {
	ID     uint32
	Pane   uint32 // non-zero for leaves
	Dir    Dir
	A, B   *Node
	Ratio  float64 // share of the space given to A
	parent *Node
}

func (n *Node) leaf() bool { return n.A == nil }

// Tree is one tab's layout.
type Tree struct {
	Root   *Node
	nextID uint32
}

// New returns a tree holding a single pane.
func New(pane uint32) *Tree {
	t := &Tree{}
	t.Root = t.newLeaf(pane)
	return t
}

func (t *Tree) newLeaf(pane uint32) *Node {
	t.nextID++
	return &Node{ID: t.nextID, Pane: pane}
}

// Empty reports whether the tree has no panes left.
func (t *Tree) Empty() bool { return t.Root == nil }

// Panes returns pane IDs in reading order (depth first, A before B).
func (t *Tree) Panes() []uint32 {
	var out []uint32
	var walk func(*Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		if n.leaf() {
			out = append(out, n.Pane)
			return
		}
		walk(n.A)
		walk(n.B)
	}
	walk(t.Root)
	return out
}

func (t *Tree) find(pane uint32) *Node {
	var found *Node
	var walk func(*Node)
	walk = func(n *Node) {
		if n == nil || found != nil {
			return
		}
		if n.leaf() {
			if n.Pane == pane {
				found = n
			}
			return
		}
		walk(n.A)
		walk(n.B)
	}
	walk(t.Root)
	return found
}

func (t *Tree) findNode(id uint32) *Node {
	var found *Node
	var walk func(*Node)
	walk = func(n *Node) {
		if n == nil || found != nil {
			return
		}
		if n.ID == id {
			found = n
			return
		}
		walk(n.A)
		walk(n.B)
	}
	walk(t.Root)
	return found
}

// Split divides pane's space in two, placing newPane after it (to the right
// for Row, below for Column). It reports false if pane isn't in the tree.
func (t *Tree) Split(pane, newPane uint32, dir Dir) bool {
	old := t.find(pane)
	if old == nil {
		return false
	}
	a := t.newLeaf(pane)
	b := t.newLeaf(newPane)
	// Turn the old leaf into the split node in place, so its parent link and
	// position stay valid.
	old.Pane, old.Dir, old.Ratio = 0, dir, 0.5
	old.A, old.B = a, b
	a.parent, b.parent = old, old
	return true
}

// Remove deletes pane; its sibling takes over the parent's space. It reports
// false if pane isn't in the tree.
func (t *Tree) Remove(pane uint32) bool {
	n := t.find(pane)
	if n == nil {
		return false
	}
	p := n.parent
	if p == nil {
		t.Root = nil
		return true
	}
	sib := p.A
	if sib == n {
		sib = p.B
	}
	// Replace the parent with the sibling.
	sib.parent = p.parent
	switch {
	case p.parent == nil:
		t.Root = sib
	case p.parent.A == p:
		p.parent.A = sib
	default:
		p.parent.B = sib
	}
	return true
}

// Layout computes each pane's rectangle and the dividers within area.
func (t *Tree) Layout(area Rect) (map[uint32]Rect, []Divider) {
	rects := map[uint32]Rect{}
	var divs []Divider
	var walk func(*Node, Rect)
	walk = func(n *Node, r Rect) {
		if n.leaf() {
			rects[n.Pane] = r
			return
		}
		ra, rb, d := split(n, r)
		divs = append(divs, d)
		walk(n.A, ra)
		walk(n.B, rb)
	}
	if t.Root != nil {
		walk(t.Root, area)
	}
	return rects, divs
}

// split computes the two child rectangles and the divider of a split node.
func split(n *Node, r Rect) (Rect, Rect, Divider) {
	if n.Dir == Row {
		a := share(r.W, n.Ratio)
		return Rect{r.X, r.Y, a, r.H},
			Rect{r.X + a + 1, r.Y, max(r.W-a-1, 0), r.H},
			Divider{Node: n.ID, X: r.X + a, Y: r.Y, Len: r.H, Vertical: true}
	}
	a := share(r.H, n.Ratio)
	return Rect{r.X, r.Y, r.W, a},
		Rect{r.X, r.Y + a + 1, r.W, max(r.H-a-1, 0)},
		Divider{Node: n.ID, X: r.X, Y: r.Y + a, Len: r.W}
}

// share is how many of total cells (minus one for the divider) child A gets.
func share(total int, ratio float64) int {
	usable := total - 1
	if usable < 2 {
		return max(usable, 0)
	}
	a := int(math.Round(float64(usable) * ratio))
	return min(max(a, 1), usable-1)
}

// nodeRect finds the rectangle a node occupies within area.
func (t *Tree) nodeRect(target *Node, area Rect) (Rect, bool) {
	var walk func(*Node, Rect) (Rect, bool)
	walk = func(n *Node, r Rect) (Rect, bool) {
		if n == target {
			return r, true
		}
		if n.leaf() {
			return Rect{}, false
		}
		ra, rb, _ := split(n, r)
		if got, ok := walk(n.A, ra); ok {
			return got, true
		}
		return walk(n.B, rb)
	}
	if t.Root == nil {
		return Rect{}, false
	}
	return walk(t.Root, area)
}

// SetDivider moves split node id's divider to absolute coordinate pos (an X
// for Row splits, a Y for Column splits). Used for dragging with the mouse.
func (t *Tree) SetDivider(id uint32, pos int, area Rect) bool {
	n := t.findNode(id)
	if n == nil || n.leaf() {
		return false
	}
	r, ok := t.nodeRect(n, area)
	if !ok {
		return false
	}
	start, total := r.X, r.W
	if n.Dir == Column {
		start, total = r.Y, r.H
	}
	if total < 3 {
		return false
	}
	a := min(max(pos-start, 1), total-2)
	n.Ratio = float64(a) / float64(total-1)
	return true
}

// Resize moves the divider nearest to pane in direction d by cells. For
// example Left moves the closest vertical divider bordering pane to the left.
func (t *Tree) Resize(pane uint32, d Direction, cells int, area Rect) bool {
	n := t.find(pane)
	if n == nil {
		return false
	}
	want := Row
	if d == Up || d == Down {
		want = Column
	}
	for p := n.parent; p != nil; p = p.parent {
		if p.Dir != want {
			continue
		}
		r, ok := t.nodeRect(p, area)
		if !ok {
			return false
		}
		start, total := r.X, r.W
		if want == Column {
			start, total = r.Y, r.H
		}
		delta := cells
		if d == Left || d == Up {
			delta = -cells
		}
		return t.SetDivider(p.ID, start+share(total, p.Ratio)+delta, area)
	}
	return false
}

// Neighbor returns the pane next to from in direction d, given the rects from
// Layout. When several panes border from, the one sharing the longest edge
// wins. It returns 0 if there is none.
func Neighbor(rects map[uint32]Rect, from uint32, d Direction) uint32 {
	fr, ok := rects[from]
	if !ok {
		return 0
	}
	best, bestOverlap := uint32(0), 0
	for id, r := range rects {
		if id == from {
			continue
		}
		var adjacent bool
		var overlap int
		switch d {
		case Left:
			adjacent = r.X+r.W+1 == fr.X
			overlap = span(r.Y, r.H, fr.Y, fr.H)
		case Right:
			adjacent = fr.X+fr.W+1 == r.X
			overlap = span(r.Y, r.H, fr.Y, fr.H)
		case Up:
			adjacent = r.Y+r.H+1 == fr.Y
			overlap = span(r.X, r.W, fr.X, fr.W)
		case Down:
			adjacent = fr.Y+fr.H+1 == r.Y
			overlap = span(r.X, r.W, fr.X, fr.W)
		}
		if adjacent && (overlap > bestOverlap || overlap == bestOverlap && overlap > 0 && id < best) {
			best, bestOverlap = id, overlap
		}
	}
	return best
}

func span(a, alen, b, blen int) int {
	return max(0, min(a+alen, b+blen)-max(a, b))
}
