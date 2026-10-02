package client

import (
	"image/color"
	"math"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
)

// luminance is WCAG 2 relative luminance.
func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	ch := func(v uint32) float64 {
		s := float64(v) / 0xffff
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(r) + 0.7152*ch(g) + 0.0722*ch(b)
}

// contrast is the WCAG contrast ratio, from 1 to 21.
func contrast(a, b color.Color) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// TestThemesAreReadable holds every theme to WCAG contrast: 4.5:1 for text
// (AA), 7:1 for body text on its main backgrounds (AAA), 3:1 for large or
// bold UI text and ANSI colours, and a visible step for dividers.
func TestThemesAreReadable(t *testing.T) {
	names := map[string]bool{}
	for _, th := range Themes {
		if !(strings.HasSuffix(th.Name, "ite") || strings.HasPrefix(th.Name, "osmite-")) || names[th.Name] {
			t.Errorf("theme name %q: must be unique, a mineral ending in -ite or osmite-*", th.Name)
		}
		names[th.Name] = true

		check := func(what string, fg, bg color.Color, min float64) {
			if fg == nil || bg == nil {
				t.Errorf("%s: %s: colour missing", th.Name, what)
				return
			}
			if r := contrast(fg, bg); r < min {
				t.Errorf("%s: %s contrast %.2f, want ≥ %.1f", th.Name, what, r, min)
			}
		}
		// Chrome.
		check("text on bar", th.Text, th.Bar, 7)
		check("text on surface", th.Text, th.Surface, 7)
		check("text on raised", th.Text, th.Raised, 4.5)
		check("muted on bar", th.Muted, th.Bar, 4.5)
		check("muted on surface", th.Muted, th.Surface, 4.5)
		check("faint on bar", th.Faint, th.Bar, 2.5)
		check("faint on surface", th.Faint, th.Surface, 2.5)
		check("on-accent on accent", th.OnAccent, th.Accent, 4.5)
		check("accent on bar", th.Accent, th.Bar, 3)
		check("accent on surface", th.Accent, th.Surface, 3)
		check("attention chip", AttnText, th.Attn, 4.5)
		check("divider on pane", th.Border, th.Bg, 1.4)
		check("focused divider on pane", th.Accent, th.Bg, 3)

		// Panes.
		check("pane text", th.Fg, th.Bg, 7)
		for i, c := range th.ANSI {
			switch i {
			case 0: // black is a background colour by convention
			case 8: // bright black is for dim text: comments, hints
				check("ANSI bright black", c, th.Bg, 2.5)
			default:
				check("ANSI "+ansiNames[i], c, th.Bg, 3)
			}
		}
	}
	for _, want := range []string{"graphite", "osmite-dark", "calcite", "sugilite"} {
		if !names[want] {
			t.Errorf("missing theme %s", want)
		}
	}
}

var ansiNames = [16]string{
	"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white",
	"bright black", "bright red", "bright green", "bright yellow", "bright blue", "bright magenta", "bright cyan", "bright white",
}

func TestPaneCellRecolours(t *testing.T) {
	th := ThemeByName("calcite")
	c := th.paneCell(&uv.Cell{Content: "x", Width: 1, Style: uv.Style{Fg: ansi.Red}})
	if c.Style.Fg != th.ANSI[1] || c.Style.Bg != th.Bg {
		t.Fatalf("got fg=%v bg=%v", c.Style.Fg, c.Style.Bg)
	}
	rgb := hex(0x123456)
	c = th.paneCell(&uv.Cell{Content: "x", Width: 1, Style: uv.Style{Fg: rgb, Bg: ansi.IndexedColor(200)}})
	if c.Style.Fg != rgb || c.Style.Bg != ansi.IndexedColor(200) {
		t.Fatal("true colour and 256-colour cells keep their colours")
	}
}

func TestWorkingDotBlinks(t *testing.T) {
	defer func() { now = time.Now }()
	th := ThemeByName("graphite")
	at := func(ms int64) uv.Style {
		now = func() time.Time { return time.UnixMilli(ms) }
		_, st := statusGlyph(th, protocol.StatusWorking)
		return st
	}
	if a, b := at(0), at(blinkInterval.Milliseconds()); a.Fg == b.Fg {
		t.Fatal("working dot should alternate colours")
	}
	now = func() time.Time { return time.UnixMilli(0) }
	_, w0 := statusGlyph(th, protocol.StatusWaiting)
	now = func() time.Time { return time.UnixMilli(blinkInterval.Milliseconds()) }
	_, w1 := statusGlyph(th, protocol.StatusWaiting)
	if w0.Fg != w1.Fg {
		t.Fatal("waiting dot should stay steady")
	}
}

func TestWrap(t *testing.T) {
	got := wrap("otmux 9.9.9 is installed. It starts next time you run otmux.", 20)
	want := []string{"otmux 9.9.9 is", "installed. It starts", "next time you run", "otmux."}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("wrap = %q", got)
	}
	if got := wrap("", 10); len(got) != 1 || got[0] != "" {
		t.Fatalf("empty line: %q", got)
	}
}
