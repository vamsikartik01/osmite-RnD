package remote

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
)

// Close codes the server ends the device socket with.
const (
	closeReplaced  websocket.StatusCode = 4000 // a newer connection for this device took over
	closeForbidden websocket.StatusCode = 4403 // the hello didn't match the token
	closeRevoked   websocket.StatusCode = 4410 // revoked from the portal
)

const (
	helloTimeout = 10 * time.Second
	writeTimeout = 30 * time.Second
	maxBackoff   = 30 * time.Second
	// sessionQueue is how many frames from a browser may wait for the
	// daemon before the session is dropped as stuck.
	sessionQueue = 1024
)

// Agent keeps the daemon's connection to the remote server while remote
// mode is on. Each browser session the server opens becomes a new local
// client: serve gets one end of an in-memory pipe and speaks the ordinary
// otmux protocol on it, and the agent copies frames between the pipe and
// the socket, tagged with the session id.
type Agent struct {
	serve    func(net.Conn)
	onStatus func(protocol.RemoteStatus)

	reload sync.Mutex // serializes Reload and Stop

	mu      sync.Mutex
	running *run
	status  protocol.RemoteStatus
}

// run is one start of remote mode with one account.
type run struct {
	acct   Account
	cancel context.CancelFunc
	done   chan struct{}
}

// NewAgent returns a stopped agent. serve runs a local client on a
// connection; onStatus is told about every status change and must not call
// back into the agent.
func NewAgent(serve func(net.Conn), onStatus func(protocol.RemoteStatus)) *Agent {
	return &Agent{serve: serve, onStatus: onStatus, status: protocol.RemoteStatus{State: protocol.RemoteOff}}
}

// Status returns the current status.
func (g *Agent) Status() protocol.RemoteStatus {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.status
}

func (g *Agent) setStatus(fn func(*protocol.RemoteStatus)) {
	g.mu.Lock()
	st := g.status
	fn(&st)
	changed := st != g.status
	g.status = st
	g.mu.Unlock()
	if changed {
		g.onStatus(st)
	}
}

// Reload reads remote.json and starts, restarts or stops remote mode to
// match it.
func (g *Agent) Reload() {
	g.reload.Lock()
	defer g.reload.Unlock()
	a, err := Load()
	if err != nil {
		log.Printf("remote: %v", err)
	}
	want := err == nil && a.Enabled && a.Linked()
	g.mu.Lock()
	cur := g.running
	g.mu.Unlock()
	if cur != nil && want && cur.acct.Token == a.Token && cur.acct.Server == a.Server && cur.acct.DeviceID == a.DeviceID {
		select {
		case <-cur.done: // it stopped by itself (revoked, replaced); start again
		default:
			return
		}
	}
	g.stopLocked()
	if !want {
		g.setStatus(func(s *protocol.RemoteStatus) { *s = protocol.RemoteStatus{State: protocol.RemoteOff} })
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &run{acct: a, cancel: cancel, done: make(chan struct{})}
	g.mu.Lock()
	g.running = r
	g.mu.Unlock()
	g.setStatus(func(s *protocol.RemoteStatus) {
		*s = protocol.RemoteStatus{State: protocol.RemoteConnecting}
	})
	go func() {
		defer close(r.done)
		g.loop(ctx, a)
	}()
}

// Stop ends remote mode, e.g. when the daemon exits.
func (g *Agent) Stop() {
	g.reload.Lock()
	defer g.reload.Unlock()
	g.stopLocked()
}

func (g *Agent) stopLocked() {
	g.mu.Lock()
	r := g.running
	g.running = nil
	g.mu.Unlock()
	if r != nil {
		r.cancel()
		<-r.done
	}
}

// errStop ends the loop for good; its message is shown to the user.
type errStop struct{ msg string }

func (e errStop) Error() string { return e.msg }

// loop connects and reconnects with backoff until ctx ends or the server
// says to stop.
func (g *Agent) loop(ctx context.Context, a Account) {
	backoff := time.Second
	for {
		start := time.Now()
		err := g.connect(ctx, a)
		if ctx.Err() != nil {
			return
		}
		var stop errStop
		if errors.As(err, &stop) {
			log.Printf("remote: stopped: %s", stop.msg)
			g.setStatus(func(s *protocol.RemoteStatus) {
				*s = protocol.RemoteStatus{State: protocol.RemoteStopped, Error: stop.msg}
			})
			return
		}
		if time.Since(start) > time.Minute {
			backoff = time.Second // it was up for a while; this is a fresh outage
		}
		wait := backoff + time.Duration(rand.Int64N(int64(backoff)/4+1))
		log.Printf("remote: disconnected (%v), retrying in %s", err, wait.Round(100*time.Millisecond))
		g.setStatus(func(s *protocol.RemoteStatus) {
			*s = protocol.RemoteStatus{State: protocol.RemoteRetrying, Error: friendly(err)}
		})
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		backoff = min(backoff*2, maxBackoff)
		g.setStatus(func(s *protocol.RemoteStatus) { s.State = protocol.RemoteConnecting })
	}
}

// friendly shortens a connection error for the settings panel.
func friendly(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 && len(msg) > 80 {
		msg = msg[i+2:]
	}
	return msg
}

// revoked forgets the token after the server said it is no longer valid,
// and turns remote mode off, with a notice for the settings panel.
func revoked(msg string) error {
	if _, err := Update(func(a *Account) {
		a.Token, a.Email, a.Enabled, a.Notice = "", "", false, msg
	}); err != nil {
		log.Printf("remote: forgetting the token: %v", err)
	}
	return errStop{msg}
}

func wsURL(server string) (string, error) {
	u, err := checkServer(server)
	if err != nil {
		return "", err
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/device/ws"
	return u.String(), nil
}

// connect holds one connection until it drops.
func (g *Agent) connect(ctx context.Context, a Account) error {
	target, err := wsURL(a.Server)
	if err != nil {
		return errStop{err.Error()}
	}
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	c, resp, err := websocket.Dial(dctx, target, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + a.Token}},
	})
	cancel()
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			return revoked("This device's link is no longer valid (revoked from the portal?). Connect the account again.")
		}
		return err
	}
	defer c.CloseNow()
	c.SetReadLimit(protocol.MaxFrameSize + 4)

	m := machine(a.DeviceID)
	hello, _ := json.Marshal(struct {
		Type string `json:"type"`
		meta
	}{"hello", m})
	hctx, cancel := context.WithTimeout(ctx, helloTimeout)
	err = c.Write(hctx, websocket.MessageText, hello)
	cancel()
	if err != nil {
		return err
	}
	log.Printf("remote: online at %s as %s", a.Server, a.DeviceID)
	g.setStatus(func(s *protocol.RemoteStatus) {
		*s = protocol.RemoteStatus{State: protocol.RemoteOnline}
	})

	m2 := &mux{c: c, ctx: ctx, serve: g.serve, sessions: map[uint32]*session{}, onCount: func(n int) {
		g.setStatus(func(s *protocol.RemoteStatus) { s.Sessions = n })
	}}
	defer m2.closeAll()
	err = m2.readLoop()
	switch websocket.CloseStatus(err) {
	case closeRevoked:
		return revoked("Revoked from the portal. Connect the account again to use remote mode.")
	case closeForbidden:
		return errStop{"The server rejected this device (its id doesn't match the token). Disconnect and connect the account again."}
	case closeReplaced:
		return errStop{"Another otmux using this account file took over the remote connection."}
	}
	return err
}

// mux routes one socket's messages to and from its browser sessions.
type mux struct {
	c       *websocket.Conn
	ctx     context.Context
	serve   func(net.Conn)
	onCount func(int)

	mu       sync.Mutex
	sessions map[uint32]*session
}

// session is one browser, seen by the daemon as a local client on pipe.
type session struct {
	id   uint32
	pipe net.Conn    // our end; the daemon serves the other
	in   chan []byte // closed by drop, under mux.mu
}

type control struct {
	Type    string `json:"type"`
	Session uint32 `json:"session"`
	User    string `json:"user,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

func (m *mux) readLoop() error {
	for {
		typ, data, err := m.c.Read(m.ctx)
		if err != nil {
			return err
		}
		if typ == websocket.MessageText {
			var msg control
			if json.Unmarshal(data, &msg) != nil {
				continue
			}
			switch msg.Type {
			case "open":
				m.open(msg.Session, msg.User)
			case "close":
				m.drop(msg.Session, false, "")
			}
			continue
		}
		sid, frame, ok := splitMsg(data)
		if !ok {
			continue
		}
		// Send under m.mu: drop closes s.in under it too.
		m.mu.Lock()
		s, stuck := m.sessions[sid], false
		if s != nil {
			select {
			case s.in <- frame:
			default:
				stuck = true
			}
		}
		m.mu.Unlock()
		if stuck {
			log.Printf("remote: session %d is stuck, closing it", sid)
			m.drop(sid, true, "otmux fell behind on this session")
		}
	}
}

// splitMsg undoes the session prefix and checks the frame's length header.
func splitMsg(m []byte) (uint32, []byte, bool) {
	if len(m) < 4+5 {
		return 0, nil, false
	}
	frame := m[4:]
	n := binary.BigEndian.Uint32(frame)
	if n == 0 || n > protocol.MaxFrameSize || int(n) != len(frame)-4 {
		return 0, nil, false
	}
	return binary.BigEndian.Uint32(m), frame, true
}

func (m *mux) open(sid uint32, user string) {
	m.mu.Lock()
	if old := m.sessions[sid]; old != nil {
		m.mu.Unlock()
		m.drop(sid, false, "")
		m.mu.Lock()
	}
	ours, theirs := net.Pipe()
	s := &session{id: sid, pipe: ours, in: make(chan []byte, sessionQueue)}
	m.sessions[sid] = s
	n := len(m.sessions)
	m.mu.Unlock()
	log.Printf("remote: session %d opened by %s", sid, user)
	m.onCount(n)

	go m.serve(theirs)
	go func() { // browser -> daemon
		for f := range s.in {
			if _, err := s.pipe.Write(f); err != nil {
				return
			}
		}
	}()
	go func() { // daemon -> browser
		for {
			f, err := readFrame(s.pipe)
			if err != nil {
				m.drop(sid, true, "otmux ended the session")
				return
			}
			msg := make([]byte, 4+len(f))
			binary.BigEndian.PutUint32(msg, sid)
			copy(msg[4:], f)
			wctx, cancel := context.WithTimeout(m.ctx, writeTimeout)
			err = m.c.Write(wctx, websocket.MessageBinary, msg)
			cancel()
			if err != nil {
				m.c.CloseNow() // the read loop sees it and reconnects
				return
			}
		}
	}()
}

// drop ends a session. tell sends the server a close for it, when the
// daemon side ended it rather than the browser.
func (m *mux) drop(sid uint32, tell bool, reason string) {
	m.mu.Lock()
	s := m.sessions[sid]
	delete(m.sessions, sid)
	n := len(m.sessions)
	if s != nil {
		close(s.in)
	}
	m.mu.Unlock()
	if s == nil {
		return
	}
	s.pipe.Close()
	log.Printf("remote: session %d closed", sid)
	m.onCount(n)
	if tell {
		msg, _ := json.Marshal(control{Type: "close", Session: sid, Reason: reason})
		wctx, cancel := context.WithTimeout(m.ctx, writeTimeout)
		_ = m.c.Write(wctx, websocket.MessageText, msg)
		cancel()
	}
}

func (m *mux) closeAll() {
	m.mu.Lock()
	ids := make([]uint32, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.drop(id, false, "")
	}
}

// readFrame reads one whole frame, header included, as the server wants it.
func readFrame(r net.Conn) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 || n > protocol.MaxFrameSize {
		return nil, fmt.Errorf("bad frame length %d", n)
	}
	f := make([]byte, 4+n)
	copy(f, hdr[:])
	if _, err := io.ReadFull(r, f[4:]); err != nil {
		return nil, err
	}
	return f, nil
}
