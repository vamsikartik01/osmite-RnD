// Package keys turns key presses into otmux actions. It implements the
// tmux-style prefix: press the prefix key (Ctrl+B by default), then a binding
// key. Every other key goes straight to the focused pane.
//
// Keyboard, mouse and the command palette all produce the same Action
// values, so behaviour is identical whichever way the user drives it.
package keys

import (
	"runtime"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
)

// Action is something the user asked otmux to do. Daemon-side actions reuse
// the protocol action names; client-only actions are defined here.
type Action struct {
	Name string
	Arg  string
	Dir  string // new-workspace: folder for its shells
}

// Client-only actions, handled without a round trip to the daemon.
const (
	ActionDetach                = "detach"
	ActionSendPrefix            = "send-prefix" // pass the prefix key itself to the pane
	ActionPalette               = "command-palette"
	ActionChooseWorkspace       = "choose-workspace"
	ActionPromptNewTab          = "prompt-new-tab"
	ActionSettings              = "settings"
	ActionToggleSidebar         = "toggle-sidebar"
	ActionPromptRenameTab       = "prompt-rename-tab"
	ActionPromptRenameWorkspace = "prompt-rename-workspace"
	ActionPromptNewWorkspace    = "prompt-new-workspace"
)

// Binding maps a key (in ultraviolet's MatchString syntax, e.g. "c", "%",
// "ctrl+b", "left") to an action. Repeatable bindings can be pressed again
// without the prefix for a moment, like tmux's resize keys.
type Binding struct {
	Key        string
	Action     Action
	Repeatable bool
}

// Keymap is the active set of bindings.
type Keymap struct {
	Prefix   string
	Bindings []Binding
}

func a(name string) Action             { return Action{Name: name} }
func aa(name, arg string) Action       { return Action{Name: name, Arg: arg} }
func b(key string, act Action) Binding { return Binding{Key: key, Action: act} }
func r(key string, act Action) Binding { return Binding{Key: key, Action: act, Repeatable: true} }

// Default returns the keymap with the given prefix (empty means ctrl+b).
//
// The primary keys need no Shift after the prefix and are mnemonic: v / h
// split, arrows move, Shift+arrows resize. The classic tmux keys (% " & $
// ( ) and friends) still work as aliases, so muscle memory carries over.
// The first binding listed for an action is the one shown in hints.
func Default(prefix string) Keymap {
	if prefix == "" {
		prefix = "ctrl+b"
	}
	km := Keymap{Prefix: prefix, Bindings: []Binding{
		// Panes.
		b("v", a(protocol.ActionSplitRight)), // vertical divider: side by side
		b("h", a(protocol.ActionSplitDown)),  // horizontal divider: stacked
		r("left", a(protocol.ActionFocusLeft)),
		r("right", a(protocol.ActionFocusRight)),
		r("up", a(protocol.ActionFocusUp)),
		r("down", a(protocol.ActionFocusDown)),
		b("o", a(protocol.ActionNextPane)),
		b("z", a(protocol.ActionZoom)),
		b("x", a(protocol.ActionClosePane)),
		r("shift+left", aa(protocol.ActionResizeL, "2")),
		r("shift+right", aa(protocol.ActionResizeR, "2")),
		r("shift+up", aa(protocol.ActionResizeU, "1")),
		r("shift+down", aa(protocol.ActionResizeD, "1")),
		r("alt+left", aa(protocol.ActionResizeL, "8")),
		r("alt+right", aa(protocol.ActionResizeR, "8")),
		r("alt+up", aa(protocol.ActionResizeU, "4")),
		r("alt+down", aa(protocol.ActionResizeD, "4")),

		// Tabs.
		b("c", a(ActionPromptNewTab)),
		b("t", a(ActionPromptNewTab)),
		b("n", a(protocol.ActionNextTab)),
		b("p", a(protocol.ActionPrevTab)),
		b("l", a(protocol.ActionLastTab)),
		b("r", a(ActionPromptRenameTab)),
		b("q", a(protocol.ActionCloseTab)),

		// Watch list (across workspaces).
		b("m", a(protocol.ActionTogglePin)),
		r("tab", a(protocol.ActionNextPinned)),
		r("shift+tab", a(protocol.ActionPrevPinned)),

		// Workspaces.
		b("w", a(ActionChooseWorkspace)),
		b("a", a(ActionPromptNewWorkspace)),
		b("e", a(ActionPromptRenameWorkspace)),
		b("[", a(protocol.ActionPrevWorkspace)),
		b("]", a(protocol.ActionNextWorkspace)),

		// otmux.
		b("s", a(ActionSettings)),
		b("b", a(ActionToggleSidebar)),
		b("space", a(ActionPalette)),
		b("/", a(ActionPalette)),
		b("d", a(ActionDetach)),
		b(prefix, a(ActionSendPrefix)),

		// tmux aliases.
		b("%", a(protocol.ActionSplitRight)),
		b("|", a(protocol.ActionSplitRight)),
		b(`"`, a(protocol.ActionSplitDown)),
		b("-", a(protocol.ActionSplitDown)),
		b(",", a(ActionPromptRenameTab)),
		b("&", a(protocol.ActionCloseTab)),
		b("C", a(ActionPromptNewWorkspace)),
		b("$", a(ActionPromptRenameWorkspace)),
		b("(", a(protocol.ActionPrevWorkspace)),
		b(")", a(protocol.ActionNextWorkspace)),
		b(":", a(ActionPalette)),
		b("?", a(ActionPalette)),
		r("ctrl+left", aa(protocol.ActionResizeL, "2")),
		r("ctrl+right", aa(protocol.ActionResizeR, "2")),
		r("ctrl+up", aa(protocol.ActionResizeU, "1")),
		r("ctrl+down", aa(protocol.ActionResizeD, "1")),
	}}
	for _, d := range "0123456789" {
		km.Bindings = append(km.Bindings, b(string(d), aa(protocol.ActionSelectTab, string(d))))
	}
	return km
}

// Hints are the keys shown in the status bar while the prefix is pending.
var Hints = []struct{ Key, Label string }{
	{"v", "split │"}, {"h", "split ─"}, {"←→", "focus"}, {"⇧←→", "resize"},
	{"z", "zoom"}, {"x", "close"}, {"c", "tab"}, {"tab", "next watched"}, {"m", "watch"}, {"w", "workspaces"},
	{"s", "settings"}, {"b", "sidebar"}, {"space", "all commands"}, {"d", "detach"},
}

// KeyFor returns the first key bound to act, or "".
func (km Keymap) KeyFor(act Action) string {
	for _, bd := range km.Bindings {
		if bd.Action == act {
			return bd.Key
		}
	}
	return ""
}

// Command describes an action for the command palette and help.
type Command struct {
	Group  string
	Title  string
	Action Action
}

// Catalog lists every action a user can run, in palette order.
var Catalog = []Command{
	{"Pane", "Split side by side", a(protocol.ActionSplitRight)},
	{"Pane", "Split stacked", a(protocol.ActionSplitDown)},
	{"Pane", "Focus left", a(protocol.ActionFocusLeft)},
	{"Pane", "Focus right", a(protocol.ActionFocusRight)},
	{"Pane", "Focus up", a(protocol.ActionFocusUp)},
	{"Pane", "Focus down", a(protocol.ActionFocusDown)},
	{"Pane", "Focus next", a(protocol.ActionNextPane)},
	{"Pane", "Zoom / unzoom", a(protocol.ActionZoom)},
	{"Pane", "Grow left", aa(protocol.ActionResizeL, "2")},
	{"Pane", "Grow right", aa(protocol.ActionResizeR, "2")},
	{"Pane", "Grow up", aa(protocol.ActionResizeU, "1")},
	{"Pane", "Grow down", aa(protocol.ActionResizeD, "1")},
	{"Pane", "Close pane", a(protocol.ActionClosePane)},
	{"Tab", "New tab…", a(ActionPromptNewTab)},
	{"Tab", "Next tab", a(protocol.ActionNextTab)},
	{"Tab", "Previous tab", a(protocol.ActionPrevTab)},
	{"Tab", "Last tab", a(protocol.ActionLastTab)},
	{"Tab", "Rename tab…", a(ActionPromptRenameTab)},
	{"Tab", "Close tab", a(protocol.ActionCloseTab)},
	{"Watch", "Watch / unwatch tab", a(protocol.ActionTogglePin)},
	{"Watch", "Next watched tab", a(protocol.ActionNextPinned)},
	{"Watch", "Previous watched tab", a(protocol.ActionPrevPinned)},
	{"Workspace", "Switch workspace…", a(ActionChooseWorkspace)},
	{"Workspace", "New workspace…", a(ActionPromptNewWorkspace)},
	{"Workspace", "Rename workspace…", a(ActionPromptRenameWorkspace)},
	{"Workspace", "Next workspace", a(protocol.ActionNextWorkspace)},
	{"Workspace", "Previous workspace", a(protocol.ActionPrevWorkspace)},
	{"Workspace", "Kill workspace", a(protocol.ActionKillWorkspace)},
	{"otmux", "Settings…", a(ActionSettings)},
	{"otmux", "Show / hide sidebar", a(ActionToggleSidebar)},
	{"Session", "Detach", a(ActionDetach)},
	{"Session", "Kill server (all workspaces)", a(protocol.ActionKillServer)},
}

// Result says what to do with a key press.
type Result int

const (
	Forward  Result = iota // send the key to the pane
	Consumed               // the key was used (or swallowed) by otmux
	Run                    // run the returned Action
)

// repeatWindow is how long a repeatable binding stays live without the
// prefix (tmux's repeat-time).
const repeatWindow = 600 * time.Millisecond

// Machine tracks whether the prefix is pending.
type Machine struct {
	km          Keymap
	pending     bool
	repeatUntil time.Time
	now         func() time.Time
}

// NewMachine returns a Machine for km.
func NewMachine(km Keymap) *Machine { return &Machine{km: km, now: time.Now} }

// Pending reports whether the prefix was pressed and a binding key is awaited.
func (m *Machine) Pending() bool { return m.pending }

// Keymap returns the machine's keymap.
func (m *Machine) Keymap() Keymap { return m.km }

// Handle processes one key press.
func (m *Machine) Handle(k uv.KeyPressEvent) (Result, Action) {
	if IsModifierOnly(k) {
		// Holding Shift to type '%' after the prefix must not cancel it.
		return Consumed, Action{}
	}
	if !m.pending {
		if m.now().Before(m.repeatUntil) {
			for _, bd := range m.km.Bindings {
				if bd.Repeatable && k.MatchString(bd.Key) {
					m.repeatUntil = m.now().Add(repeatWindow)
					return Run, bd.Action
				}
			}
		}
		m.repeatUntil = time.Time{}
		if k.MatchString(m.km.Prefix) {
			m.pending = true
			return Consumed, Action{}
		}
		return Forward, Action{}
	}
	m.pending = false
	for _, bd := range m.km.Bindings {
		if k.MatchString(bd.Key) {
			if bd.Repeatable {
				m.repeatUntil = m.now().Add(repeatWindow)
			}
			return Run, bd.Action
		}
	}
	return Consumed, Action{} // unknown binding: swallow it, like tmux
}

// Cancel clears a pending prefix.
func (m *Machine) Cancel() { m.pending = false }

// IsModifierOnly reports whether k is a bare modifier press (Shift, Ctrl,
// Caps Lock, ...). Windows reports these as key events; terminals on Unix
// don't. They never produce input for the pane.
func IsModifierOnly(k uv.KeyPressEvent) bool {
	switch k.Code {
	case uv.KeyLeftShift, uv.KeyRightShift, uv.KeyLeftCtrl, uv.KeyRightCtrl,
		uv.KeyLeftAlt, uv.KeyRightAlt, uv.KeyLeftMeta, uv.KeyRightMeta,
		uv.KeyLeftSuper, uv.KeyRightSuper, uv.KeyLeftHyper, uv.KeyRightHyper,
		uv.KeyIsoLevel3Shift, uv.KeyIsoLevel5Shift,
		uv.KeyCapsLock, uv.KeyNumLock, uv.KeyScrollLock:
		return true
	}
	return false
}

// Label renders a key for display: "ctrl+b" becomes "⌃B" on macOS and
// "Ctrl+B" elsewhere; arrows become ← → ↑ ↓.
func Label(key string) string {
	mac := runtime.GOOS == "darwin"
	parts := strings.Split(key, "+")
	var out strings.Builder
	for i, p := range parts {
		last := i == len(parts)-1
		if !last {
			switch p {
			case "ctrl":
				out.WriteString(pick(mac, "⌃", "Ctrl+"))
			case "alt":
				out.WriteString(pick(mac, "⌥", "Alt+"))
			case "shift":
				out.WriteString(pick(mac, "⇧", "Shift+"))
			default:
				out.WriteString(p + "+")
			}
			continue
		}
		switch p {
		case "left":
			p = "←"
		case "right":
			p = "→"
		case "up":
			p = "↑"
		case "down":
			p = "↓"
		case "space":
			p = "Space"
		case "tab":
			p = "Tab"
		case "":
			p = "+" // the key was "+" itself
		default:
			if len(parts) > 1 && len(p) == 1 {
				p = strings.ToUpper(p)
			}
		}
		out.WriteString(p)
	}
	return out.String()
}

func pick(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}
