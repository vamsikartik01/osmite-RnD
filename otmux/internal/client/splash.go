package client

import (
	"image/color"
	"math"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/mattn/go-runewidth"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/keys"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
)

// logoGlyphs spell "otmux" as a 10-pixel-tall bitmap with 2-pixel strokes,
// drawn with half blocks so every pixel is square.
var logoGlyphs = [][]string{
	{ // o
		"........",
		"........",
		"..####..",
		".######.",
		"###..###",
		"##....##",
		"###..###",
		".######.",
		"..####..",
		"........",
	},
	{ // t
		".##....",
		".##....",
		"######.",
		"######.",
		".##....",
		".##....",
		".##....",
		".###.##",
		"..####.",
		".......",
	},
	{ // m
		"............",
		"............",
		"##.###..###.",
		"#######.####",
		"##...##...##",
		"##...##...##",
		"##...##...##",
		"##...##...##",
		"##...##...##",
		"............",
	},
	{ // u
		"........",
		"........",
		"##....##",
		"##....##",
		"##....##",
		"##....##",
		"###..###",
		".#######",
		"..###.##",
		"........",
	},
	{ // x
		"........",
		"........",
		"##....##",
		".##..##.",
		"..####..",
		"...##...",
		"..####..",
		".##..##.",
		"##....##",
		"........",
	},
}

// logoBitmap joins the glyphs, scaled by n, into rows of pixels.
func logoBitmap(n int) [][]bool {
	h := len(logoGlyphs[0])
	var rows [][]bool
	for y := 0; y < h; y++ {
		var row []bool
		for i, g := range logoGlyphs {
			if i > 0 {
				row = append(row, false, false)
			}
			for _, ch := range g[y] {
				row = append(row, ch == '#')
			}
		}
		var scaled []bool
		for _, px := range row {
			for range n {
				scaled = append(scaled, px)
			}
		}
		for range n {
			rows = append(rows, scaled)
		}
	}
	return rows
}

var logos = map[int][][]bool{1: logoBitmap(1), 2: logoBitmap(2)}

// drawSplash shows the logo and a few keys in a fresh pane (one nobody has
// typed in yet), centred in the blank space below the shell's prompt. It
// draws nothing if there isn't room.
func (c *Client) drawSplash(scr uv.Screen, p protocol.PaneInfo, m *mirror, bg color.Color) {
	if m == nil {
		return
	}
	// The shell's own output (banner, prompt) occupies the top rows.
	used := 0
	for y := 0; y < min(p.H, m.emu.Height()); y++ {
		for x := 0; x < min(p.W, m.emu.Width()); x++ {
			if cell := m.emu.CellAt(x, y); cell != nil && strings.TrimSpace(cell.Content) != "" {
				used = y + 1
				break
			}
		}
	}
	free := p.H - used - 1
	t := c.theme
	prefix := keys.Label(c.keys.Keymap().Prefix)
	hints := [][2]string{{"v", "split"}, {"c", "new tab"}, {"w", "workspaces"}, {"space", "all commands"}}
	hintW := runewidth.StringWidth(prefix) + 10
	for _, h := range hints {
		hintW += runewidth.StringWidth(keys.Label(h[0])+" "+h[1]) + 5
	}

	for _, scale := range []int{2, 1} {
		bm := logos[scale]
		logoW, logoH := len(bm[0]), (len(bm)+1)/2
		blockH := logoH + 5 // logo, gap, tagline, byline, gap, hints
		if logoW > p.W-4 || blockH > free {
			continue
		}
		y0 := p.Y + used + (free-blockH)/2 + 1
		x0 := p.X + (p.W-logoW)/2
		for row := 0; row < logoH; row++ {
			for x := 0; x < logoW; x++ {
				top := bm[2*row][x]
				bot := 2*row+1 < len(bm) && bm[2*row+1][x]
				glyph := ""
				switch {
				case top && bot:
					glyph = "█"
				case top:
					glyph = "▀"
				case bot:
					glyph = "▄"
				default:
					continue
				}
				fg := gradient(t, float64(x)/float64(logoW))
				scr.SetCell(x0+x, y0+row, &uv.Cell{Content: glyph, Width: 1, Style: uv.Style{Fg: fg, Bg: bg}})
			}
		}
		center := func(y int, segs [][2]any) {
			w := 0
			for _, sg := range segs {
				w += runewidth.StringWidth(sg[0].(string))
			}
			x := p.X + max((p.W-w)/2, 0)
			for _, sg := range segs {
				st := sg[1].(uv.Style)
				if st.Bg == nil {
					st.Bg = bg
				}
				x = put(scr, x, y, sg[0].(string), st)
			}
		}
		center(y0+logoH+1, [][2]any{{"terminal workspaces", uv.Style{Fg: t.Muted}}})
		center(y0+logoH+2, [][2]any{{"from osmite RnD", uv.Style{Fg: t.Faint}}})
		if hintW <= p.W-2 {
			// Keys drawn as keycaps, after the prefix that comes first.
			cap := uv.Style{Fg: t.Text, Bg: t.Raised, Attrs: uv.AttrBold}
			segs := [][2]any{{" " + prefix + " ", cap}, {"  then  ", uv.Style{Fg: t.Faint}}}
			for i, h := range hints {
				if i > 0 {
					segs = append(segs, [2]any{"   ", uv.Style{}})
				}
				segs = append(segs, [2]any{" " + keys.Label(h[0]) + " ", cap}, [2]any{" " + h[1], uv.Style{Fg: t.Muted}})
			}
			center(y0+logoH+4, segs)
		}
		return
	}
}

// gradient is the colour at f (0..1) along otmux's gradient: from the
// theme's accent to the accent with its hue turned 50 degrees, so it suits
// every theme (purple to pink, blue to violet, green to teal).
func gradient(t Theme, f float64) color.Color {
	h, s, l := toHSL(t.Accent)
	return fromHSL(h+50*f, s, l)
}

// toHSL converts c to hue (degrees), saturation and lightness (0..1).
func toHSL(c color.Color) (h, s, l float64) {
	r16, g16, b16, _ := c.RGBA()
	r, g, b := float64(r16)/65535, float64(g16)/65535, float64(b16)/65535
	hi, lo := max(r, g, b), min(r, g, b)
	l = (hi + lo) / 2
	d := hi - lo
	if d == 0 {
		return 0, 0, l
	}
	s = d / (1 - math.Abs(2*l-1))
	switch hi {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	return math.Mod(h*60+360, 360), s, l
}

// fromHSL is the inverse of toHSL.
func fromHSL(h, s, l float64) color.Color {
	h = math.Mod(h+360, 360)
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch {
	case h < 60:
		r, g = c, x
	case h < 120:
		r, g = x, c
	case h < 180:
		g, b = c, x
	case h < 240:
		g, b = x, c
	case h < 300:
		r, b = x, c
	default:
		r, b = c, x
	}
	u := func(v float64) uint8 { return uint8(math.Round(min(max(v+m, 0), 1) * 255)) }
	return color.RGBA{R: u(r), G: u(g), B: u(b), A: 0xff}
}

// mix blends a towards b by f (0..1).
func mix(a, b color.Color, f float64) color.Color {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	l := func(x, y uint32) uint8 { return uint8((float64(x)*(1-f) + float64(y)*f) / 257) }
	return color.RGBA{R: l(ar, br), G: l(ag, bg), B: l(ab, bb), A: 0xff}
}
