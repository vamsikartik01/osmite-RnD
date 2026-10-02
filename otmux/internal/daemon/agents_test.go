package daemon_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/daemon"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
)

// fakeAgent is a stand-in for a coding agent. It spins (prints) for 8s, and
// meanwhile sends a progress-bar update (OSC 9;4, which must be ignored) and
// at 4s a desktop notification (OSC 9), the way agents ask for permission.
// Then it waits quietly.
const fakeAgent = `package main

import (
	"fmt"
	"time"
)

func main() {
	for i := 0; i < 80; i++ {
		fmt.Printf("\rthinking %d", i)
		if i == 5 {
			fmt.Print("\x1b]9;4;1;50\x07")
		}
		if i == 40 {
			fmt.Print("\x1b]9;Claude needs your permission\x07")
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Println("\ndone")
	time.Sleep(60 * time.Second)
}
`

// TestAgentPinning runs a program called "claude" in a pane and checks its
// tab pins itself, shows working, waiting on a notification and idle once
// it stops in view, can be jumped to from another tab, and stays unpinned once the user unpins it.
func TestAgentPinning(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a fake agent")
	}
	dir := shortTempDir(t)
	src := filepath.Join(dir, "fake", "main.go")
	if err := os.MkdirAll(filepath.Dir(src), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(fakeAgent), 0o600); err != nil {
		t.Fatal(err)
	}
	agent := filepath.Join(dir, "claude")
	if runtime.GOOS == "windows" {
		agent += ".exe"
	}
	build := exec.Command("go", "build", "-o", agent, src)
	build.Dir = filepath.Dir(src)
	build.Env = append(os.Environ(), "GO111MODULE=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake agent: %v\n%s", err, out)
	}

	sock := filepath.Join(dir, "a.sock")
	t.Setenv("OTMUX_SOCKET", sock)
	t.Setenv("OTMUX_CONFIG", filepath.Join(t.TempDir(), "config.json")) // keeps the real remote.json out
	if runtime.GOOS == "windows" {
		t.Setenv("OTMUX_SHELL", "cmd.exe")
	} else {
		t.Setenv("OTMUX_SHELL", "/bin/sh")
	}
	ran := make(chan error, 1)
	go func() { ran <- daemon.Run() }()
	t.Cleanup(func() {
		k := attachRaw(t, sock)
		_ = k.Send(protocol.TypeCommand, protocol.Command{Action: protocol.ActionKillServer})
		<-ran
	})

	c := attach(t, sock)
	st := c.waitState(t, func(s protocol.State) bool { return len(s.Tabs) == 1 && len(s.Panes) == 1 })
	agentTab := st.Tabs[0].ID
	typeLine(t, c, st.ActivePane, agent)
	started := time.Now()

	// Look away straight away: open another tab.
	c.send(t, protocol.TypeCommand, protocol.Command{Action: protocol.ActionNewTab, Arg: "other"})

	// The agent's tab joins the watch list as working; the progress-bar
	// update must not flag it.
	c.waitState(t, func(s protocol.State) bool {
		return len(s.Pinned) == 1 && s.Pinned[0].TabID == agentTab && s.Pinned[0].Agent == "claude" &&
			s.Pinned[0].Status == protocol.StatusWorking && s.Active == 1
	})

	// Its notification flags it as waiting for us, even though it's still
	// spinning: well before going quiet (8s) could have.
	c.waitState(t, func(s protocol.State) bool {
		return len(s.Pinned) == 1 && s.Pinned[0].Status == protocol.StatusWaiting
	})
	if d := time.Since(started); d > 7*time.Second {
		t.Fatalf("waiting status took %v; the notification should have set it at ~4s", d)
	}

	// Jumping to the next pinned tab goes back to it and clears the flag.
	c.send(t, protocol.TypeCommand, protocol.Command{Action: protocol.ActionNextPinned})
	c.waitState(t, func(s protocol.State) bool {
		return s.Active == 0 && len(s.Pinned) == 1 && s.Pinned[0].Status != protocol.StatusWaiting
	})

	// It stops spinning while we're looking, so it's idle, not waiting.
	c.waitState(t, func(s protocol.State) bool {
		return s.Active == 0 && len(s.Pinned) == 1 && s.Pinned[0].Status == protocol.StatusIdle
	})

	// Unpinning by hand sticks even though the agent is still running.
	c.send(t, protocol.TypeCommand, protocol.Command{Action: protocol.ActionTogglePin})
	c.waitState(t, func(s protocol.State) bool { return len(s.Pinned) == 0 })
	time.Sleep(2 * time.Second) // a couple of agent scans
	c.send(t, protocol.TypeCommand, protocol.Command{Action: protocol.ActionNextTab})
	st = c.waitState(t, func(s protocol.State) bool { return s.Active == 1 })
	if len(st.Pinned) != 0 {
		t.Fatalf("unpinned agent tab came back: %+v", st.Pinned)
	}

	// And any tab can be pinned by hand; with no agent it's just idle.
	c.send(t, protocol.TypeCommand, protocol.Command{Action: protocol.ActionTogglePin})
	c.waitState(t, func(s protocol.State) bool {
		return len(s.Pinned) == 1 && s.Pinned[0].Name == "other" && s.Pinned[0].Agent == "" &&
			s.Pinned[0].Status == protocol.StatusIdle
	})
}
