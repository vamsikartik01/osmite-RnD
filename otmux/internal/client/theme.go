package client

import (
	"image/color"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Theme is a complete colour scheme: otmux's chrome (bars, sidebar,
// panels) and the panes themselves (default background and text, and the
// 16 ANSI colours programs use), like a Windows Terminal colour scheme.
// TestThemesAreReadable checks every theme against WCAG contrast rules.
type Theme struct {
	Name string

	// Chrome.
	Bar      color.Color // status bars and sidebar background
	Surface  color.Color // panels (settings, palette)
	Raised   color.Color // active tab, selected rows, buttons
	Text     color.Color
	Muted    color.Color
	Faint    color.Color
	Accent   color.Color // focus, current workspace
	OnAccent color.Color // text on Accent
	Attn     color.Color // prefix pending, zoom; text on it is AttnText
	Border   color.Color // inactive pane divider

	// Panes.
	Bg   color.Color
	Fg   color.Color
	ANSI [16]color.Color // black red green yellow blue magenta cyan white, then bright
}

// AttnText is the text colour on Attn chips in every theme.
var AttnText = hex(0x1c1c1e)

func hex(v uint32) color.Color {
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

func pal(v ...uint32) (p [16]color.Color) {
	for i, c := range v {
		p[i] = hex(c)
	}
	return p
}

// Themes are named after minerals ending in "-ite", plus osmite's own. The
// first is the default.
var Themes = []Theme{
	{ // osmite's own: graphite's greys with a purple accent.
		Name: "osmite-dark",
		Bar:  hex(0x1c1c1e), Surface: hex(0x2c2c2e), Raised: hex(0x3a3a3c),
		Text: hex(0xf5f5f7), Muted: hex(0xa1a1a6), Faint: hex(0x6e6e73),
		Accent: hex(0xbf5af2), OnAccent: hex(0x000000), Attn: hex(0xff9f0a), Border: hex(0x48484a),
		Bg: hex(0x141416), Fg: hex(0xe5e5ea),
		ANSI: pal(0x2c2c2e, 0xff453a, 0x32d74b, 0xffd60a, 0x409cff, 0xbf5af2, 0x64d2ff, 0xd1d1d6,
			0x7c7c80, 0xff6961, 0x5de57a, 0xffe066, 0x70b4ff, 0xda8fff, 0x8ae0ff, 0xffffff),
	},
	{ // Apple-style dark: quiet greys, one blue accent.
		Name: "graphite",
		Bar:  hex(0x1c1c1e), Surface: hex(0x2c2c2e), Raised: hex(0x3a3a3c),
		Text: hex(0xf5f5f7), Muted: hex(0xa1a1a6), Faint: hex(0x6e6e73),
		Accent: hex(0x0a84ff), OnAccent: hex(0x000000), Attn: hex(0xff9f0a), Border: hex(0x48484a),
		Bg: hex(0x141416), Fg: hex(0xe5e5ea),
		ANSI: pal(0x2c2c2e, 0xff453a, 0x32d74b, 0xffd60a, 0x409cff, 0xbf5af2, 0x64d2ff, 0xd1d1d6,
			0x7c7c80, 0xff6961, 0x5de57a, 0xffe066, 0x70b4ff, 0xda8fff, 0x8ae0ff, 0xffffff),
	},
	{ // Light, Apple-style.
		Name: "calcite",
		Bar:  hex(0xf2f2f7), Surface: hex(0xffffff), Raised: hex(0xe5e5ea),
		Text: hex(0x1c1c1e), Muted: hex(0x5e5e63), Faint: hex(0x8e8e93),
		Accent: hex(0x0062cc), OnAccent: hex(0xffffff), Attn: hex(0xff9500), Border: hex(0xc7c7cc),
		Bg: hex(0xffffff), Fg: hex(0x1c1c1e),
		ANSI: pal(0x1c1c1e, 0xc4001a, 0x1d7a33, 0x8a5300, 0x0040dd, 0x8e2fb0, 0x00708f, 0x6e6e73,
			0x636366, 0xd70015, 0x248a3d, 0x9c6000, 0x0050f0, 0xa040c8, 0x00809f, 0x48484a),
	},
	{ // Deep navy with sky blue.
		Name: "azurite",
		Bar:  hex(0x0f172a), Surface: hex(0x1e293b), Raised: hex(0x334155),
		Text: hex(0xf1f5f9), Muted: hex(0xa3b1c6), Faint: hex(0x64748b),
		Accent: hex(0x38bdf8), OnAccent: hex(0x0f172a), Attn: hex(0xfbbf24), Border: hex(0x334155),
		Bg: hex(0x0b1120), Fg: hex(0xcbd5e1),
		ANSI: pal(0x1e293b, 0xf87171, 0x4ade80, 0xfacc15, 0x60a5fa, 0xc084fc, 0x22d3ee, 0xcbd5e1,
			0x64748b, 0xfca5a5, 0x86efac, 0xfde047, 0x93c5fd, 0xd8b4fe, 0x67e8f9, 0xf8fafc),
	},
	{ // Forest green.
		Name: "malachite",
		Bar:  hex(0x0d1f1a), Surface: hex(0x14302a), Raised: hex(0x1f4038),
		Text: hex(0xecf7f3), Muted: hex(0x9cc4b6), Faint: hex(0x5f8a7c),
		Accent: hex(0x34d399), OnAccent: hex(0x052e22), Attn: hex(0xfbbf24), Border: hex(0x2a5247),
		Bg: hex(0x0a1814), Fg: hex(0xd1e7df),
		ANSI: pal(0x14302a, 0xf87171, 0x34d399, 0xfbbf24, 0x60a5fa, 0xe879f9, 0x2dd4bf, 0xd1e7df,
			0x5f8a7c, 0xfca5a5, 0x6ee7b7, 0xfcd34d, 0x93c5fd, 0xf0abfc, 0x5eead4, 0xf0fdf4),
	},
	{ // Warm rose.
		Name: "rhodonite",
		Bar:  hex(0x1f1418), Surface: hex(0x2d1c22), Raised: hex(0x42272f),
		Text: hex(0xfdf0f3), Muted: hex(0xd0a5b2), Faint: hex(0x8f6875),
		Accent: hex(0xfb7185), OnAccent: hex(0x2a0a12), Attn: hex(0xfcd34d), Border: hex(0x55333d),
		Bg: hex(0x180f12), Fg: hex(0xf5dde4),
		ANSI: pal(0x2d1c22, 0xfb7185, 0x86efac, 0xfcd34d, 0x93c5fd, 0xf0abfc, 0x67e8f9, 0xf5dde4,
			0x8f6875, 0xfda4af, 0xbbf7d0, 0xfde68a, 0xbfdbfe, 0xf5d0fe, 0xa5f3fc, 0xfff1f2),
	},
	{ // Gold on charcoal.
		Name: "pyrite",
		Bar:  hex(0x1a1814), Surface: hex(0x26231c), Raised: hex(0x36322a),
		Text: hex(0xf7f1e3), Muted: hex(0xbdb399), Faint: hex(0x7f7766),
		Accent: hex(0xe8b84a), OnAccent: hex(0x1a1814), Attn: hex(0xf97316), Border: hex(0x4a4436),
		Bg: hex(0x14120e), Fg: hex(0xede4cf),
		ANSI: pal(0x26231c, 0xef6f5e, 0xa3c46c, 0xe8b84a, 0x7fa7d6, 0xc896c8, 0x7cc7b8, 0xede4cf,
			0x7f7766, 0xf4917f, 0xbfd98a, 0xf2cf72, 0xa3c2e6, 0xdcb3dc, 0xa0dccf, 0xfffaf0),
	},
	{ // Purple.
		Name: "sugilite",
		Bar:  hex(0x1a1325), Surface: hex(0x251b33), Raised: hex(0x362848),
		Text: hex(0xf6f0ff), Muted: hex(0xbcaad9), Faint: hex(0x7f6d9e),
		Accent: hex(0xb57bff), OnAccent: hex(0x1a1325), Attn: hex(0xffb454), Border: hex(0x46365c),
		Bg: hex(0x130e1c), Fg: hex(0xe6dcf7),
		ANSI: pal(0x251b33, 0xff6b8b, 0x7ee7a8, 0xffd36b, 0x8ab4ff, 0xc792ff, 0x6be3e3, 0xe6dcf7,
			0x7f6d9e, 0xff94ab, 0xa6f0c4, 0xffe29a, 0xb1cbff, 0xdcb8ff, 0x9cf0f0, 0xffffff),
	},
}

// ThemeByName returns the named theme, or the default.
func ThemeByName(name string) Theme {
	for _, t := range Themes {
		if strings.EqualFold(t.Name, name) {
			return t
		}
	}
	return Themes[0]
}

// paneCell recolours a pane cell with the theme: default colours become the
// theme's background and text, and the 16 ANSI colours its palette.
// 256-colour (above 15) and true-colour cells keep the program's colours.
func (t Theme) paneCell(c *uv.Cell) *uv.Cell {
	c = c.Clone()
	c.Style.Fg = t.mapColor(c.Style.Fg, t.Fg)
	c.Style.Bg = t.mapColor(c.Style.Bg, t.Bg)
	return c
}

func (t Theme) mapColor(c, def color.Color) color.Color {
	switch v := c.(type) {
	case nil:
		return def
	case ansi.BasicColor:
		if int(v) < len(t.ANSI) {
			return t.ANSI[v]
		}
	case ansi.IndexedColor:
		if int(v) < len(t.ANSI) {
			return t.ANSI[v]
		}
	}
	return c
}
