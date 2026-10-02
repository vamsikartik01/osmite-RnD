// Package protocol defines the wire format between otmux clients and the
// otmux daemon.
//
// Every message is a frame:
//
//	[4 bytes big-endian length][1 byte type][payload]
//
// The length covers the type byte and the payload. Control messages carry a
// JSON payload. Pane output is the hot path, so it uses a compact binary
// payload instead: [4 bytes big-endian pane ID][raw bytes].
//
// The same protocol is meant to be carried over a remote transport later, so
// it never assumes the client and daemon share a machine.
package protocol

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
)

// Version is bumped whenever the wire format changes incompatibly.
const Version = 5

// MaxFrameSize bounds a single frame so a corrupt length can't make us
// allocate gigabytes.
const MaxFrameSize = 16 << 20

// Type identifies the kind of message in a frame.
type Type uint8

const (
	// Client -> daemon.
	TypeHello   Type = 1 // Hello
	TypeInput   Type = 2 // Input
	TypeResize  Type = 3 // Resize
	TypeCommand Type = 4 // Command
	TypeList    Type = 5 // no payload; answered with TypeListReply

	// Daemon -> client.
	TypeWelcome   Type = 64 // Welcome
	TypeState     Type = 65 // State
	TypeOutput    Type = 66 // binary, see EncodeOutput
	TypeSnapshot  Type = 67 // Snapshot
	TypeBye       Type = 68 // Bye
	TypeListReply Type = 69 // ListReply
	TypeHistory   Type = 70 // History
)

// Attach modes for Hello.
const (
	AttachOrCreate = ""       // attach, creating the workspace if missing
	AttachExisting = "attach" // fail if the workspace doesn't exist
	CreateNew      = "new"    // fail if the workspace already exists
)

// Hello is the first message a client sends.
type Hello struct {
	Version   int    `json:"version"`
	Workspace string `json:"workspace"`
	Mode      string `json:"mode,omitempty"`
	Dir       string `json:"dir,omitempty"` // client's working directory, for new workspaces
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
}

// Welcome acknowledges a Hello. If Error is set the daemon closes the
// connection after sending it.
type Welcome struct {
	Version int    `json:"version"`
	Error   string `json:"error,omitempty"`
	// Daemon is the daemon's otmux version, e.g. "1.0.0"; clients use it to
	// tell when an update is waiting for the daemon to restart.
	Daemon string `json:"daemon,omitempty"`
}

// Key is a keyboard event, independent of any terminal encoding. The daemon
// encodes it for the target pane, taking that pane's terminal modes (such as
// application cursor keys) into account.
type Key struct {
	Code        rune   `json:"code"`
	Text        string `json:"text,omitempty"`
	Mod         int    `json:"mod,omitempty"`
	ShiftedCode rune   `json:"shifted,omitempty"`
	BaseCode    rune   `json:"base,omitempty"`
}

// MouseKind says what a Mouse event is.
type MouseKind string

const (
	MouseClick   MouseKind = "click"
	MouseRelease MouseKind = "release"
	MouseWheel   MouseKind = "wheel"
	MouseMotion  MouseKind = "motion"
)

// Mouse is a mouse event in pane-relative cell coordinates.
type Mouse struct {
	Kind   MouseKind `json:"kind"`
	X      int       `json:"x"`
	Y      int       `json:"y"`
	Button int       `json:"button"`
	Mod    int       `json:"mod,omitempty"`
}

// Input sends exactly one of Key, Paste or Mouse to a pane.
type Input struct {
	Pane  uint32 `json:"pane"`
	Key   *Key   `json:"key,omitempty"`
	Paste string `json:"paste,omitempty"`
	Mouse *Mouse `json:"mouse,omitempty"`
}

// Resize reports the client's terminal size.
type Resize struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// Command asks the daemon to perform a workspace action.
type Command struct {
	Action string `json:"action"`
	Arg    string `json:"arg,omitempty"`
	Dir    string `json:"dir,omitempty"` // new-workspace: where its shells start
}

// Command actions understood by the daemon.
const (
	// Tabs.
	ActionNewTab    = "new-tab" // Arg: name; empty picks a random one
	ActionNextTab   = "next-tab"
	ActionPrevTab   = "prev-tab"
	ActionLastTab   = "last-tab"
	ActionSelectTab = "select-tab" // Arg: tab index
	ActionCloseTab  = "close-tab"
	ActionRenameTab = "rename-tab" // Arg: new name

	// Panes.
	ActionSplitRight = "split-right" // side by side (tmux prefix %)
	ActionSplitDown  = "split-down"  // stacked (tmux prefix ")
	ActionFocusLeft  = "focus-left"
	ActionFocusRight = "focus-right"
	ActionFocusUp    = "focus-up"
	ActionFocusDown  = "focus-down"
	ActionFocusPane  = "focus-pane" // Arg: pane ID
	ActionNextPane   = "next-pane"
	ActionClosePane  = "close-pane"
	ActionZoom       = "zoom"
	ActionResizeL    = "resize-left"  // Arg: cells
	ActionResizeR    = "resize-right" // Arg: cells
	ActionResizeU    = "resize-up"    // Arg: cells
	ActionResizeD    = "resize-down"  // Arg: cells
	ActionDragSplit  = "drag-split"   // Arg: "<node> <pos>"

	// Workspaces.
	ActionNewWorkspace    = "new-workspace"    // Arg: name
	ActionSwitchWorkspace = "switch-workspace" // Arg: name
	ActionNextWorkspace   = "next-workspace"
	ActionPrevWorkspace   = "prev-workspace"
	ActionRenameWorkspace = "rename-workspace" // Arg: new name
	ActionKillWorkspace   = "kill-workspace"   // Arg: name (empty: current)

	// Scrollback.
	ActionScrollReset = "scroll-reset" // Arg: pane ID; return to the live screen

	// Pinned tabs, across workspaces.
	ActionTogglePin  = "toggle-pin"  // pin or unpin the active tab
	ActionGotoTab    = "goto-tab"    // Arg: tab ID, in any workspace
	ActionNextPinned = "next-pinned" // cycle through pinned tabs
	ActionPrevPinned = "prev-pinned"

	ActionKillServer = "kill-server"
)

// TabInfo describes one tab in a State message.
type TabInfo struct {
	ID     uint32 `json:"id"`
	Name   string `json:"name"`
	Panes  int    `json:"panes"`
	Pinned bool   `json:"pinned,omitempty"`
	Status string `json:"status,omitempty"` // for pinned tabs; see PinnedTab
}

// Pinned tab statuses.
const (
	StatusWorking = "working" // an agent is producing output
	StatusWaiting = "waiting" // an agent went quiet while nobody was watching
	StatusIdle    = "idle"
)

// PinnedTab is a tab in the pinned list, which spans all workspaces. Tabs
// running a coding agent pin themselves; any tab can be pinned by hand.
type PinnedTab struct {
	TabID     uint32 `json:"tab"`
	Name      string `json:"name"`
	Workspace string `json:"workspace"`
	Agent     string `json:"agent,omitempty"` // e.g. "claude"
	Status    string `json:"status"`
}

// PaneInfo places a visible pane within the pane area (the client's
// terminal minus the status bar).
type PaneInfo struct {
	ID    uint32 `json:"id"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
	W     int    `json:"w"`
	H     int    `json:"h"`
	Title string `json:"title,omitempty"`
	// Mouse is set when the program in the pane asked for mouse events
	// (vim, htop, ...). Clients then forward clicks instead of selecting text.
	Mouse bool `json:"mouse,omitempty"`
	// Fresh is set until the user first types in the pane; clients show a
	// welcome splash in it meanwhile.
	Fresh bool `json:"fresh,omitempty"`
}

// Divider is a line between panes. Node identifies it for dragging.
type Divider struct {
	Node     uint32 `json:"node"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	Len      int    `json:"len"`
	Vertical bool   `json:"vertical,omitempty"`
}

// State is everything a client needs to draw the workspace except pane
// contents. It is sent whenever anything visible changes.
type State struct {
	Workspace  string          `json:"workspace"`
	Workspaces []WorkspaceInfo `json:"workspaces"`
	Tabs       []TabInfo       `json:"tabs"`
	Active     int             `json:"active"`
	Panes      []PaneInfo      `json:"panes"` // visible panes of the active tab
	Dividers   []Divider       `json:"dividers"`
	ActivePane uint32          `json:"active_pane"`
	Pinned     []PinnedTab     `json:"pinned,omitempty"`
	Zoomed     bool            `json:"zoomed,omitempty"`
}

// Snapshot replaces the client's copy of a pane's screen. Data is a stream of
// VT sequences that redraws the screen from blank; it is followed by live
// TypeOutput frames for the same pane.
type Snapshot struct {
	Pane         uint32 `json:"pane"`
	Cols         int    `json:"cols"`
	Rows         int    `json:"rows"`
	Data         string `json:"data"`
	CursorX      int    `json:"cursor_x"`
	CursorY      int    `json:"cursor_y"`
	CursorHidden bool   `json:"cursor_hidden,omitempty"`
}

// Bye tells the client the daemon is ending the connection, and why.
type Bye struct {
	Reason string `json:"reason"`
}

// WorkspaceInfo describes one workspace.
type WorkspaceInfo struct {
	Name    string `json:"name"`
	Tabs    int    `json:"tabs"`
	Clients int    `json:"clients"`
}

// History shows a pane's scrollback to one client after it scrolled with
// the mouse wheel. Data redraws the pane-sized view like a Snapshot does.
// Offset 0 means the client is back at the live screen and should drop the
// history view.
type History struct {
	Pane   uint32 `json:"pane"`
	Offset int    `json:"offset"` // lines above the live screen
	Total  int    `json:"total"`  // scrollback lines available
	Cols   int    `json:"cols"`
	Rows   int    `json:"rows"`
	Data   string `json:"data"`
}

// ListReply answers TypeList.
type ListReply struct {
	Workspaces []WorkspaceInfo `json:"workspaces"`
}

// EncodeOutput builds a TypeOutput payload.
func EncodeOutput(pane uint32, data []byte) []byte {
	buf := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(buf, pane)
	copy(buf[4:], data)
	return buf
}

// DecodeOutput splits a TypeOutput payload.
func DecodeOutput(payload []byte) (pane uint32, data []byte, err error) {
	if len(payload) < 4 {
		return 0, nil, errors.New("protocol: short output frame")
	}
	return binary.BigEndian.Uint32(payload), payload[4:], nil
}

// Frame is one decoded message.
type Frame struct {
	Type    Type
	Payload []byte
}

// Decode unmarshals a JSON payload into v.
func (f Frame) Decode(v any) error {
	if err := json.Unmarshal(f.Payload, v); err != nil {
		return fmt.Errorf("protocol: decoding type %d: %w", f.Type, err)
	}
	return nil
}

// EncodeFrame serializes a frame including its header.
func EncodeFrame(t Type, payload []byte) []byte {
	buf := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(buf, uint32(1+len(payload)))
	buf[4] = byte(t)
	copy(buf[5:], payload)
	return buf
}

// EncodeJSON serializes v as a JSON frame.
func EncodeJSON(t Type, v any) ([]byte, error) {
	payload, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("protocol: encoding type %d: %w", t, err)
	}
	return EncodeFrame(t, payload), nil
}

// ReadFrame reads one frame from r.
func ReadFrame(r io.Reader) (Frame, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Frame{}, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 || n > MaxFrameSize {
		return Frame{}, fmt.Errorf("protocol: bad frame length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return Frame{}, err
	}
	return Frame{Type: Type(buf[0]), Payload: buf[1:]}, nil
}

// Conn wraps a net.Conn with buffered frame reads and serialized writes.
type Conn struct {
	nc net.Conn
	r  *bufio.Reader
	mu sync.Mutex
}

// NewConn wraps nc.
func NewConn(nc net.Conn) *Conn {
	return &Conn{nc: nc, r: bufio.NewReaderSize(nc, 64<<10)}
}

// Read reads the next frame.
func (c *Conn) Read() (Frame, error) { return ReadFrame(c.r) }

// WriteRaw writes an already-encoded frame.
func (c *Conn) WriteRaw(frame []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.nc.Write(frame)
	return err
}

// Send writes v as a JSON frame of type t.
func (c *Conn) Send(t Type, v any) error {
	frame, err := EncodeJSON(t, v)
	if err != nil {
		return err
	}
	return c.WriteRaw(frame)
}

// SendEmpty writes a frame with no payload.
func (c *Conn) SendEmpty(t Type) error { return c.WriteRaw(EncodeFrame(t, nil)) }

// Close closes the underlying connection.
func (c *Conn) Close() error { return c.nc.Close() }
