package remote

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
)

// fakeServer is the part of the otmux remote server a device talks to.
type fakeServer struct {
	*httptest.Server
	t *testing.T

	mu       sync.Mutex
	approved bool
	deviceID string
	polls    int
	conns    chan *websocket.Conn
	hellos   chan map[string]string
	reject   int // HTTP status to refuse the next socket with
}

const testToken = "otd_secret"

func newFakeServer(t *testing.T) *fakeServer {
	f := &fakeServer{t: t, conns: make(chan *websocket.Conn, 4), hellos: make(chan map[string]string, 4)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/device/code", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.deviceID = req["device_id"]
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code": "otc_x", "user_code": "BCDF-GHJK",
			"verification_uri_complete": f.URL + "/link?code=BCDF-GHJK", "expires_in": 600, "interval": 1,
		})
	})
	mux.HandleFunc("POST /api/device/token", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.polls++
		if !f.approved {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"device_token": testToken, "device_id": f.deviceID, "account_email": "you@example.com"})
	})
	mux.HandleFunc("GET /api/device/ws", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		reject := f.reject
		f.reject = 0
		f.mu.Unlock()
		if reject != 0 || r.Header.Get("Authorization") != "Bearer "+testToken {
			http.Error(w, "no", max(reject, http.StatusUnauthorized))
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		c.SetReadLimit(protocol.MaxFrameSize + 4)
		_, data, err := c.Read(r.Context())
		if err != nil {
			return
		}
		var hello map[string]string
		_ = json.Unmarshal(data, &hello)
		f.hellos <- hello
		f.conns <- c
		<-r.Context().Done()
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func isolate(t *testing.T, server string) {
	t.Setenv("OTMUX_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("OTMUX_REMOTE_URL", server)
}

func TestLink(t *testing.T) {
	f := newFakeServer(t)
	isolate(t, f.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	l, err := StartLink(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if l.UserCode != "BCDF-GHJK" || !strings.HasSuffix(l.URL, "/link?code=BCDF-GHJK") {
		t.Fatalf("link = %+v", l)
	}
	go func() {
		time.Sleep(1500 * time.Millisecond)
		f.mu.Lock()
		f.approved = true
		f.mu.Unlock()
	}()
	a, err := l.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if a.Token != testToken || a.Email != "you@example.com" || a.Server != f.URL || a.Enabled {
		t.Fatalf("account = %+v", a)
	}
	if f.polls < 2 {
		t.Fatalf("polled %d times, want a pending poll first", f.polls)
	}
	if a.DeviceID != f.deviceID || len(a.DeviceID) != 36 {
		t.Fatalf("device id %q, server saw %q", a.DeviceID, f.deviceID)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(Path())
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("remote.json mode %v, %v", info.Mode(), err)
		}
	}

	// Relinking keeps the device id.
	l2, err := StartLink(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if l2.deviceID != a.DeviceID {
		t.Fatalf("relink made a new device id %q, had %q", l2.deviceID, a.DeviceID)
	}
}

func TestPlainHTTPOnlyToLocalhost(t *testing.T) {
	for server, ok := range map[string]bool{
		"https://otmux.osmite.site": true, "http://localhost:8091": true, "http://127.0.0.1:1": true,
		"http://otmux.osmite.site": false, "ftp://x": false, "": false,
	} {
		if _, err := checkServer(server); (err == nil) != ok {
			t.Errorf("checkServer(%q) = %v", server, err)
		}
	}
}

// echoServe stands in for the daemon: it answers Hello with Welcome and
// echoes pastes back as pane output.
func echoServe(nc net.Conn) {
	conn := protocol.NewConn(nc)
	defer conn.Close()
	f, err := conn.Read()
	if err != nil || f.Type != protocol.TypeHello {
		return
	}
	_ = conn.Send(protocol.TypeWelcome, protocol.Welcome{Version: protocol.Version})
	for {
		f, err := conn.Read()
		if err != nil {
			return
		}
		var in protocol.Input
		if f.Type == protocol.TypeInput && f.Decode(&in) == nil {
			_ = conn.WriteRaw(protocol.EncodeFrame(protocol.TypeOutput, protocol.EncodeOutput(1, []byte(in.Paste))))
		}
	}
}

type statuses struct {
	ch chan protocol.RemoteStatus
}

func (s statuses) wait(t *testing.T, ok func(protocol.RemoteStatus) bool) protocol.RemoteStatus {
	t.Helper()
	timeout := time.After(15 * time.Second)
	for {
		select {
		case st := <-s.ch:
			if ok(st) {
				return st
			}
		case <-timeout:
			t.Fatal("timed out waiting for a remote status")
		}
	}
}

func linked(t *testing.T, server string) {
	t.Helper()
	isolate(t, server)
	if err := (Account{DeviceID: newDeviceID(), Server: server, Token: testToken, Email: "you@example.com", Enabled: true}).Save(); err != nil {
		t.Fatal(err)
	}
}

func sessionMsg(sid uint32, frame []byte) []byte {
	m := make([]byte, 4+len(frame))
	binary.BigEndian.PutUint32(m, sid)
	copy(m[4:], frame)
	return m
}

func TestAgentSessions(t *testing.T) {
	f := newFakeServer(t)
	linked(t, f.URL)
	st := statuses{make(chan protocol.RemoteStatus, 64)}
	g := NewAgent(echoServe, func(s protocol.RemoteStatus) { st.ch <- s })
	g.Reload()
	defer g.Stop()

	hello := <-f.hellos
	a, _ := Load()
	if hello["type"] != "hello" || hello["device_id"] != a.DeviceID || hello["os"] != runtime.GOOS {
		t.Fatalf("hello = %v", hello)
	}
	c := <-f.conns
	st.wait(t, func(s protocol.RemoteStatus) bool { return s.State == protocol.RemoteOnline })

	ctx := context.Background()
	open, _ := json.Marshal(map[string]any{"type": "open", "session": 7, "user": "you@example.com"})
	_ = c.Write(ctx, websocket.MessageText, open)
	st.wait(t, func(s protocol.RemoteStatus) bool { return s.Sessions == 1 })

	hf, _ := protocol.EncodeJSON(protocol.TypeHello, protocol.Hello{Version: protocol.Version, Cols: 80, Rows: 24})
	_ = c.Write(ctx, websocket.MessageBinary, sessionMsg(7, hf))
	in, _ := protocol.EncodeJSON(protocol.TypeInput, protocol.Input{Pane: 1, Paste: "hi"})
	_ = c.Write(ctx, websocket.MessageBinary, sessionMsg(7, in))

	want := []protocol.Type{protocol.TypeWelcome, protocol.TypeOutput}
	for _, typ := range want {
		rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		mt, data, err := c.Read(rctx)
		cancel()
		if err != nil || mt != websocket.MessageBinary {
			t.Fatalf("read: %v %v", mt, err)
		}
		if sid := binary.BigEndian.Uint32(data); sid != 7 {
			t.Fatalf("session %d, want 7", sid)
		}
		got, err := protocol.ReadFrame(strings.NewReader(string(data[4:])))
		if err != nil || got.Type != typ {
			t.Fatalf("frame %v %v, want type %d", got.Type, err, typ)
		}
		if typ == protocol.TypeOutput {
			if _, out, _ := protocol.DecodeOutput(got.Payload); string(out) != "hi" {
				t.Fatalf("output %q", out)
			}
		}
	}

	closeMsg, _ := json.Marshal(map[string]any{"type": "close", "session": 7})
	_ = c.Write(ctx, websocket.MessageText, closeMsg)
	st.wait(t, func(s protocol.RemoteStatus) bool { return s.Sessions == 0 })

	// A server restart: the agent comes back by itself.
	c.Close(websocket.StatusGoingAway, "restarting")
	st.wait(t, func(s protocol.RemoteStatus) bool { return s.State == protocol.RemoteRetrying })
	<-f.hellos
	<-f.conns
	st.wait(t, func(s protocol.RemoteStatus) bool { return s.State == protocol.RemoteOnline })

	// Turning remote mode off disconnects.
	if _, err := Update(func(a *Account) { a.Enabled = false }); err != nil {
		t.Fatal(err)
	}
	g.Reload()
	if s := g.Status(); s.State != protocol.RemoteOff {
		t.Fatalf("status after off = %+v", s)
	}
}

func TestAgentRevoked(t *testing.T) {
	f := newFakeServer(t)
	linked(t, f.URL)
	st := statuses{make(chan protocol.RemoteStatus, 64)}
	g := NewAgent(echoServe, func(s protocol.RemoteStatus) { st.ch <- s })
	g.Reload()
	defer g.Stop()
	<-f.hellos
	c := <-f.conns
	c.Close(closeRevoked, "device revoked")
	s := st.wait(t, func(s protocol.RemoteStatus) bool { return s.State == protocol.RemoteStopped })
	if !strings.Contains(s.Error, "Revoked") {
		t.Fatalf("error %q", s.Error)
	}
	a, _ := Load()
	if a.Linked() || a.Enabled || a.DeviceID == "" || a.Notice == "" {
		t.Fatalf("after revoke: %+v", a)
	}
}

func TestAgentUnauthorized(t *testing.T) {
	f := newFakeServer(t)
	linked(t, f.URL)
	f.reject = http.StatusUnauthorized
	st := statuses{make(chan protocol.RemoteStatus, 64)}
	g := NewAgent(echoServe, func(s protocol.RemoteStatus) { st.ch <- s })
	g.Reload()
	defer g.Stop()
	st.wait(t, func(s protocol.RemoteStatus) bool { return s.State == protocol.RemoteStopped })
	if a, _ := Load(); a.Linked() || a.Enabled {
		t.Fatalf("after 401: %+v", a)
	}
}

func TestAgentReplaced(t *testing.T) {
	f := newFakeServer(t)
	linked(t, f.URL)
	st := statuses{make(chan protocol.RemoteStatus, 64)}
	g := NewAgent(echoServe, func(s protocol.RemoteStatus) { st.ch <- s })
	g.Reload()
	defer g.Stop()
	<-f.hellos
	c := <-f.conns
	c.Close(closeReplaced, "device reconnected")
	st.wait(t, func(s protocol.RemoteStatus) bool { return s.State == protocol.RemoteStopped })
	if a, _ := Load(); !a.Linked() || !a.Enabled {
		t.Fatalf("replaced must keep the account: %+v", a)
	}
	select {
	case <-f.hellos:
		t.Fatal("reconnected after being replaced")
	case <-time.After(1500 * time.Millisecond):
	}
}

// A server that stops answering (a laptop woke up on a dead connection)
// is noticed by the agent's own pings, and it reconnects.
func TestAgentDeadConnection(t *testing.T) {
	oldI, oldT := pingInterval, pingTimeout
	pingInterval, pingTimeout = 200*time.Millisecond, 300*time.Millisecond
	defer func() { pingInterval, pingTimeout = oldI, oldT }()

	f := newFakeServer(t) // after the hello it never reads, so pings go unanswered
	linked(t, f.URL)
	st := statuses{make(chan protocol.RemoteStatus, 64)}
	g := NewAgent(echoServe, func(s protocol.RemoteStatus) { st.ch <- s })
	g.Reload()
	defer g.Stop()
	<-f.hellos
	<-f.conns
	st.wait(t, func(s protocol.RemoteStatus) bool { return s.State == protocol.RemoteRetrying })
	select {
	case <-f.hellos:
	case <-time.After(10 * time.Second):
		t.Fatal("did not reconnect after the server stopped answering")
	}
}
