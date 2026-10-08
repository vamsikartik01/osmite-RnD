package remote

import (
	"context"
	"encoding/json"
	"log"

	"github.com/coder/websocket"
)

// Remote runs: the server asks the daemon to run a kind of job (Run) and
// the daemon reports how it went (RunReport). The daemon decides what a
// kind runs; see the daemon's runs.go. Both travel as JSON text messages on
// the device socket, beside the session open/close messages.

// Report types.
const (
	RunStarted = "run_started"
	RunExited  = "run_exited"
	RunFailed  = "run_failed"
)

// RunRequest is the server's "run" message.
type RunRequest struct {
	RunID     string            `json:"run_id"`
	Workspace string            `json:"workspace"`
	TabName   string            `json:"tab_name"`
	Kind      string            `json:"kind"`
	Env       map[string]string `json:"env"`
}

// RunReport is what the daemon tells the server about a run.
type RunReport struct {
	Type     string `json:"type"`
	RunID    string `json:"run_id"`
	TabID    string `json:"tab_id,omitempty"`    // run_started
	ExitCode *int   `json:"exit_code,omitempty"` // run_exited; -1 if killed by a signal
	Code     string `json:"code,omitempty"`      // run_failed: no_launcher, busy, spawn_error, bad_request
	Message  string `json:"message,omitempty"`
}

// Runner carries out runs; the daemon implements it.
type Runner interface {
	Run(RunRequest)
	Stop(runID string)
}

// maxPendingReports bounds reports kept while the server is unreachable.
const maxPendingReports = 64

// SetRunner sets who handles run and stop messages. Without one they are
// ignored, as by otmux versions from before remote runs.
func (g *Agent) SetRunner(r Runner) {
	g.mu.Lock()
	g.runner = r
	g.mu.Unlock()
}

// Report sends a run report now, or as soon as the server is reachable
// again. It never blocks.
func (g *Agent) Report(r RunReport) {
	if g == nil {
		return
	}
	g.mu.Lock()
	if len(g.pending) >= maxPendingReports {
		g.pending = g.pending[1:]
	}
	g.pending = append(g.pending, r)
	g.mu.Unlock()
	select {
	case g.kick <- struct{}{}:
	default:
	}
}

// sendReports writes pending reports on c until ctx ends; one that can't be
// written stays pending for the next connection.
func (g *Agent) sendReports(ctx context.Context, c *websocket.Conn) {
	for {
		g.mu.Lock()
		batch := g.pending
		g.pending = nil
		g.mu.Unlock()
		for i, r := range batch {
			b, _ := json.Marshal(r)
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.Write(wctx, websocket.MessageText, b)
			cancel()
			if err != nil {
				g.mu.Lock()
				g.pending = append(batch[i:], g.pending...)
				g.mu.Unlock()
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-g.kick:
		}
	}
}

// onRunMessage handles a "run" or "stop" message from the server.
func (g *Agent) onRunMessage(typ string, data []byte) {
	g.mu.Lock()
	r := g.runner
	g.mu.Unlock()
	if r == nil {
		return
	}
	switch typ {
	case "run":
		var req RunRequest
		if err := json.Unmarshal(data, &req); err != nil {
			log.Printf("remote: bad run message: %v", err)
			return
		}
		go r.Run(req)
	case "stop":
		var m struct {
			RunID string `json:"run_id"`
		}
		if json.Unmarshal(data, &m) == nil && m.RunID != "" {
			go r.Stop(m.RunID)
		}
	}
}
