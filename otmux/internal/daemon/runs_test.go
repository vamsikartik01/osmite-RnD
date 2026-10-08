package daemon_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/daemon"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/remote"
)

// relay is a stand-in for the remote server's device socket: it takes the
// daemon's connection and lets the test send run messages and read reports.
type relay struct {
	conns  chan *websocket.Conn
	hellos chan map[string]any
}

func newRelay(t *testing.T) (*relay, string) {
	t.Helper()
	r := &relay{conns: make(chan *websocket.Conn, 4), hellos: make(chan map[string]any, 4)}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		c, err := websocket.Accept(w, req, nil)
		if err != nil {
			return
		}
		_, data, err := c.Read(req.Context())
		if err != nil {
			return
		}
		var hello map[string]any
		json.Unmarshal(data, &hello)
		r.hellos <- hello
		r.conns <- c
		<-req.Context().Done()
	}))
	t.Cleanup(ts.Close)
	return r, ts.URL
}

func (r *relay) send(t *testing.T, c *websocket.Conn, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	if err := c.Write(context.Background(), websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}

// report reads the next run report for runID, skipping anything else.
func (r *relay) report(t *testing.T, c *websocket.Conn, runID string) remote.RunReport {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for a report on %s: %v", runID, err)
		}
		if typ != websocket.MessageText {
			continue
		}
		var rep remote.RunReport
		if json.Unmarshal(data, &rep) == nil && rep.RunID == runID {
			return rep
		}
	}
}

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func attachWorkspace(t *testing.T, sock, ws string) *testClient {
	t.Helper()
	conn := attachRaw(t, sock)
	c := &testClient{conn: conn, frames: make(chan protocol.Frame, 1024), screen: map[uint32]*mirror{}}
	c.send(t, protocol.TypeHello, protocol.Hello{Version: protocol.Version, Workspace: ws, Mode: protocol.AttachExisting, Cols: 100, Rows: 30})
	go func() {
		defer close(c.frames)
		for {
			f, err := conn.Read()
			if err != nil {
				return
			}
			c.frames <- f
		}
	}()
	return c
}

func TestRemoteRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("launcher scripts here are sh")
	}
	dir := shortTempDir(t)
	sock := filepath.Join(dir, "t.sock")
	t.Setenv("OTMUX_SOCKET", sock)
	t.Setenv("OTMUX_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("OTMUX_SHELL", "/bin/sh")

	echo := writeScript(t, dir, "echo-run", `echo "run=$OTMUX_RUN_ID ws=$OTMUX_WORKSPACE kind=$OTMUX_KIND slug=$OTMUX_AGENT_SLUG"
printf 'prompt=%s\n' "$OTMUX_PROMPT"
exit 7
`)
	sleeper := writeScript(t, dir, "sleep-run", "echo sleeping\nsleep 60\n")
	cfg, _ := json.Marshal(map[string]any{"remote": map[string]any{"launchers": map[string]string{"echo": echo, "sleep": sleeper, "relative": "echo-run"}}})
	os.WriteFile(filepath.Join(dir, "config.json"), cfg, 0o600)

	rl, url := newRelay(t)
	if err := (remote.Account{DeviceID: "11111111-1111-4111-8111-111111111111", Server: url, Token: "otd_test", Email: "you@example.com", Enabled: true}).Save(); err != nil {
		t.Fatal(err)
	}
	ran := make(chan error, 1)
	go func() { ran <- daemon.Run() }()
	defer func() {
		k := attachRaw(t, sock)
		k.Send(protocol.TypeCommand, protocol.Command{Action: protocol.ActionKillServer})
		select {
		case <-ran:
		case <-time.After(10 * time.Second):
			t.Error("daemon did not exit")
		}
	}()

	hello := <-rl.hellos
	if f, _ := hello["features"].([]any); !slices.Contains(f, any("runs")) {
		t.Fatalf("hello does not offer runs: %v", hello)
	}
	c := <-rl.conns

	// A run creates its workspace, gets only the allowed env, and the
	// prompt reaches the launcher as data, never as shell code.
	prompt := `fix it; echo INJECTED $(echo NOPE) "quoted"`
	run := func(id, ws, kind string, env map[string]string) {
		rl.send(t, c, map[string]any{"type": "run", "run_id": id, "workspace": ws, "tab_name": "login redirect", "kind": kind, "env": env})
	}
	run("r1", "ofs-t", "echo", map[string]string{"OTMUX_AGENT_SLUG": "mars", "OTMUX_PROMPT": prompt})
	started := rl.report(t, c, "r1")
	if started.Type != remote.RunStarted || started.TabID == "" {
		t.Fatalf("%+v", started)
	}
	exited := rl.report(t, c, "r1")
	if exited.Type != remote.RunExited || exited.ExitCode == nil || *exited.ExitCode != 7 {
		t.Fatalf("%+v", exited)
	}

	// The finished tab stays, with the output and the exit note.
	v := attachWorkspace(t, sock, "ofs-t")
	st := v.waitState(t, func(s protocol.State) bool {
		return len(s.Tabs) == 2 && s.Tabs[s.Active].Name == "login redirect"
	})
	v.waitScreen(t, st.ActivePane, func(s string) bool { return strings.Contains(s, "[run finished, exit code 7]") })
	screen := stripANSI(v.screen[st.ActivePane].text.String())
	for _, want := range []string{"run=r1 ws=ofs-t kind=echo slug=mars", "prompt=" + prompt} {
		if !strings.Contains(screen, want) {
			t.Fatalf("screen lacks %q:\n%s", want, screen)
		}
	}

	// A repeated Run is answered with where the run is now.
	run("r1", "ofs-t", "echo", nil)
	if rep := rl.report(t, c, "r1"); rep.Type != remote.RunExited || *rep.ExitCode != 7 {
		t.Fatalf("repeat: %+v", rep)
	}

	// Failures: no launcher, a relative launcher path, a forbidden variable.
	for _, tc := range []struct {
		id, kind string
		env      map[string]string
		code     string
	}{
		{"f1", "nope", nil, "no_launcher"},
		{"f2", "relative", nil, "no_launcher"},
		{"f3", "echo", map[string]string{"PATH": "/tmp"}, "bad_request"},
		{"f4", "echo", map[string]string{"OTMUX_SOCKET": "/tmp/x"}, "bad_request"},
	} {
		run(tc.id, "ofs-f", tc.kind, tc.env)
		if rep := rl.report(t, c, tc.id); rep.Type != remote.RunFailed || rep.Code != tc.code {
			t.Fatalf("%s: %+v", tc.id, rep)
		}
	}

	// One live run per workspace; Stop kills it.
	run("s1", "ofs-s", "sleep", nil)
	if rep := rl.report(t, c, "s1"); rep.Type != remote.RunStarted {
		t.Fatalf("%+v", rep)
	}
	run("s2", "ofs-s", "sleep", nil)
	if rep := rl.report(t, c, "s2"); rep.Type != remote.RunFailed || rep.Code != "busy" {
		t.Fatalf("busy: %+v", rep)
	}
	rl.send(t, c, map[string]any{"type": "stop", "run_id": "s1"})
	if rep := rl.report(t, c, "s1"); rep.Type != remote.RunExited || *rep.ExitCode != -1 {
		t.Fatalf("stop: %+v", rep)
	}

	// The next run in ofs-t replaces the finished run's tab.
	run("r2", "ofs-t", "echo", nil)
	if rep := rl.report(t, c, "r2"); rep.Type != remote.RunStarted {
		t.Fatalf("%+v", rep)
	}
	old := mustID(t, started.TabID)
	v.waitState(t, func(s protocol.State) bool {
		return len(s.Tabs) == 2 && !slices.ContainsFunc(s.Tabs, func(tb protocol.TabInfo) bool { return tb.ID == old })
	})
}

func mustID(t *testing.T, s string) uint32 {
	t.Helper()
	var id uint32
	if err := json.Unmarshal([]byte(s), &id); err != nil {
		t.Fatal(err)
	}
	return id
}
