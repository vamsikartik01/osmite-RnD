package layout

import (
	"reflect"
	"testing"
)

var area = Rect{0, 0, 101, 31}

func TestSingle(t *testing.T) {
	tr := New(1)
	rects, divs := tr.Layout(area)
	if rects[1] != area || len(divs) != 0 {
		t.Fatalf("rects=%v divs=%v", rects, divs)
	}
}

func TestSplitRow(t *testing.T) {
	tr := New(1)
	tr.Split(1, 2, Row)
	rects, divs := tr.Layout(area)
	if rects[1] != (Rect{0, 0, 50, 31}) || rects[2] != (Rect{51, 0, 50, 31}) {
		t.Fatalf("rects=%v", rects)
	}
	if len(divs) != 1 || !divs[0].Vertical || divs[0].X != 50 || divs[0].Len != 31 {
		t.Fatalf("divs=%v", divs)
	}
}

func TestSplitColumnNested(t *testing.T) {
	tr := New(1)
	tr.Split(1, 2, Row)
	tr.Split(2, 3, Column)
	rects, divs := tr.Layout(area)
	if rects[2] != (Rect{51, 0, 50, 15}) || rects[3] != (Rect{51, 16, 50, 15}) {
		t.Fatalf("rects=%v", rects)
	}
	if len(divs) != 2 {
		t.Fatalf("divs=%v", divs)
	}
	if got := tr.Panes(); !reflect.DeepEqual(got, []uint32{1, 2, 3}) {
		t.Fatalf("panes=%v", got)
	}
}

func TestRemoveCollapses(t *testing.T) {
	tr := New(1)
	tr.Split(1, 2, Row)
	tr.Split(2, 3, Column)
	tr.Remove(2)
	rects, _ := tr.Layout(area)
	if rects[3] != (Rect{51, 0, 50, 31}) {
		t.Fatalf("pane 3 should take pane 2's space: %v", rects)
	}
	tr.Remove(1)
	rects, _ = tr.Layout(area)
	if rects[3] != area {
		t.Fatalf("pane 3 should fill the area: %v", rects)
	}
	tr.Remove(3)
	if !tr.Empty() {
		t.Fatal("tree should be empty")
	}
}

func TestNeighbor(t *testing.T) {
	tr := New(1)
	tr.Split(1, 2, Row)
	tr.Split(2, 3, Column)
	rects, _ := tr.Layout(area)
	cases := []struct {
		from uint32
		d    Direction
		want uint32
	}{
		{1, Right, 2}, // 2 and 3 tie on overlap; lower ID wins
		{2, Left, 1},
		{3, Left, 1},
		{2, Down, 3},
		{3, Up, 2},
		{1, Left, 0},
	}
	for _, c := range cases {
		if got := Neighbor(rects, c.from, c.d); got != c.want {
			t.Errorf("Neighbor(%d, %v) = %d, want %d", c.from, c.d, got, c.want)
		}
	}
}

func TestResizeAndDrag(t *testing.T) {
	tr := New(1)
	tr.Split(1, 2, Row)
	tr.Resize(1, Right, 10, area)
	rects, divs := tr.Layout(area)
	if rects[1].W != 60 || divs[0].X != 60 {
		t.Fatalf("after resize: rects=%v divs=%v", rects, divs)
	}
	tr.SetDivider(divs[0].Node, 20, area)
	rects, _ = tr.Layout(area)
	if rects[1].W != 20 || rects[2].X != 21 {
		t.Fatalf("after drag: %v", rects)
	}
	// Clamped: never smaller than one cell.
	tr.SetDivider(divs[0].Node, -5, area)
	if rects, _ = tr.Layout(area); rects[1].W != 1 {
		t.Fatalf("after clamp: %v", rects)
	}
}

func TestResizeWrongAxisIsNoop(t *testing.T) {
	tr := New(1)
	tr.Split(1, 2, Row)
	if tr.Resize(1, Up, 5, area) {
		t.Fatal("no column split exists; resize up should fail")
	}
}
