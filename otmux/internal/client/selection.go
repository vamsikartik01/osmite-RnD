package client

import (
	"encoding/base64"
	"runtime"
	"strings"

	"github.com/atotto/clipboard"
	uv "github.com/charmbracelet/ultraviolet"
)

// selection is a mouse text selection inside one pane, in pane-relative
// cells. It works like a normal terminal: drag to select, release to copy.
type selection struct {
	pane           uint32
	ax, ay, bx, by int  // anchor and current end
	moved          bool // dragged at least one cell; a plain click selects nothing
}

// ordered returns the selection's start and end in reading order.
func (s *selection) ordered() (sx, sy, ex, ey int) {
	if s.ay < s.by || s.ay == s.by && s.ax <= s.bx {
		return s.ax, s.ay, s.bx, s.by
	}
	return s.bx, s.by, s.ax, s.ay
}

// contains reports whether pane cell (x, y) is selected.
func (s *selection) contains(x, y int) bool {
	if !s.moved {
		return false
	}
	sx, sy, ex, ey := s.ordered()
	if y < sy || y > ey {
		return false
	}
	if y == sy && x < sx || y == ey && x > ex {
		return false
	}
	return true
}

// text extracts the selected text from what the pane is showing.
func (s *selection) text(m *mirror, w int) string {
	if m == nil || !s.moved {
		return ""
	}
	sx, sy, ex, ey := s.ordered()
	var lines []string
	for y := sy; y <= ey; y++ {
		x0, x1 := 0, w-1
		if y == sy {
			x0 = sx
		}
		if y == ey {
			x1 = ex
		}
		var b strings.Builder
		for x := x0; x <= x1 && x < m.emu.Width(); x++ {
			c := m.emu.CellAt(x, y)
			if c == nil || c.Width == 0 && c.Content == "" {
				continue // second half of a wide character
			}
			if c.Content == "" {
				b.WriteByte(' ')
			} else {
				b.WriteString(c.Content)
			}
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}
	nl := "\n"
	if runtime.GOOS == "windows" {
		nl = "\r\n"
	}
	return strings.Join(lines, nl)
}

// highlight inverts the selected cells already drawn for pane p.
func (s *selection) highlight(scr uv.Screen, px, py, pw, ph int) {
	for y := 0; y < ph; y++ {
		for x := 0; x < pw; x++ {
			if !s.contains(x, y) {
				continue
			}
			c := scr.CellAt(px+x, py+y)
			if c == nil {
				continue
			}
			c = c.Clone()
			c.Style.Attrs |= uv.AttrReverse
			scr.SetCell(px+x, py+y, c)
		}
	}
}

// copyText puts text on the system clipboard. If that isn't available (for
// example over SSH with no display), it asks the outer terminal to do it
// with OSC 52.
func (c *Client) copyText(text string) {
	if text == "" {
		return
	}
	if err := clipboard.WriteAll(text); err == nil {
		return
	}
	seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
	_, _ = c.term.Screen().WriteString(seq)
	c.markDirty()
}

func pasteText() string {
	s, err := clipboard.ReadAll()
	if err != nil {
		return ""
	}
	return s
}
