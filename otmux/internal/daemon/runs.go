package daemon

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/config"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/remote"
)

// Remote runs: the remote server asks the daemon to run a *kind* of job in
// a workspace (create it if needed), in a new tab. The kind maps to a
// launcher program the user configured locally (config remote.launchers);
// the server never chooses what runs, and nothing it sends becomes part of
// a command line, only environment variables of the launcher.

const (
	runKeep      = 24 * time.Hour // finished runs remembered, for repeated Run messages
	runKeepMax   = 256
	stopGrace    = 5 * time.Second
	maxRunEnv    = 32
	maxRunEnvLen = 16 << 10
	runCols      = 120 // size of a workspace a run creates, until a client attaches
	runRows      = 40
)

// Failure codes in RunFailed.
const (
	runNoLauncher = "no_launcher"
	runBusy       = "busy"
	runSpawnError = "spawn_error"
	runBadRequest = "bad_request"
)

var (
	runIDRe   = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	runKindRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	runEnvRe  = regexp.MustCompile(`^OTMUX_[A-Z0-9_]{1,40}$`)
)

// ownEnv are variables otmux itself sets or reads; a run can't set them.
var ownEnv = map[string]bool{
	"OTMUX_RUN_ID": true, "OTMUX_WORKSPACE": true, "OTMUX_KIND": true,
	"OTMUX_PANE": true, "OTMUX_SOCKET": true, "OTMUX_CONFIG": true, "OTMUX_SHELL": true,
	"OTMUX_PREFIX": true, "OTMUX_REMOTE_URL": true,
}

// remoteRun is one run. Guarded by Server.mu.
type remoteRun struct {
	id, workspace, kind string
	tabID               uint32
	pane                *Pane
	finished            bool
	report              remote.RunReport // the last thing reported, resent for a repeated Run
	at                  time.Time
}

func (r *remoteRun) live() bool { return r.pane != nil && !r.finished }

// launcher finds the program for kind in the user's config.
func launcher(kind string) (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", fmt.Errorf("reading %s: %v", config.Path(), err)
	}
	path, ok := cfg.Remote.Launchers[kind]
	if !ok || path == "" {
		return "", fmt.Errorf("no launcher for %q in %s (remote.launchers)", kind, config.Path())
	}
	if err := config.CheckLauncher(kind, path); err != nil {
		return "", err
	}
	return path, nil
}

func validRunWorkspace(name string) bool {
	if name == "" || len(name) > 32 || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// runEnv builds the launcher's environment: otmux's own variables plus
// the server's OTMUX_* ones that pass the allowlist.
func runEnv(req remote.RunRequest) ([]string, error) {
	env := []string{"OTMUX_RUN_ID=" + req.RunID, "OTMUX_WORKSPACE=" + req.Workspace, "OTMUX_KIND=" + req.Kind}
	if len(req.Env) > maxRunEnv {
		return nil, errors.New("too many environment variables")
	}
	size := 0
	for k, v := range req.Env {
		if !runEnvRe.MatchString(k) || ownEnv[k] {
			return nil, fmt.Errorf("environment variable %s is not allowed", k)
		}
		if strings.ContainsRune(v, 0) {
			return nil, fmt.Errorf("environment variable %s contains a NUL", k)
		}
		size += len(k) + len(v)
		env = append(env, k+"="+v)
	}
	if size > maxRunEnvLen {
		return nil, errors.New("environment too large")
	}
	return env, nil
}

// Run starts a remote run. It is called by the remote agent, never with
// s.mu held, and reports through s.remote.
func (s *Server) Run(req remote.RunRequest) {
	s.mu.Lock()
	rep := s.startRun(req)
	s.mu.Unlock()
	s.remote.Report(rep)
}

func (s *Server) startRun(req remote.RunRequest) remote.RunReport {
	fail := func(code, msg string) remote.RunReport {
		log.Printf("run %s: %s: %s", req.RunID, code, msg)
		return remote.RunReport{Type: remote.RunFailed, RunID: req.RunID, Code: code, Message: msg}
	}
	if !runIDRe.MatchString(req.RunID) {
		return fail(runBadRequest, "bad run id")
	}
	s.pruneRuns()
	if r := s.runs[req.RunID]; r != nil {
		return r.report // a repeat: say again where it is
	}
	if !runKindRe.MatchString(req.Kind) || !validRunWorkspace(req.Workspace) {
		return s.remember(req, fail(runBadRequest, "bad kind or workspace name"))
	}
	env, err := runEnv(req)
	if err != nil {
		return s.remember(req, fail(runBadRequest, err.Error()))
	}
	path, err := launcher(req.Kind)
	if err != nil {
		return s.remember(req, fail(runNoLauncher, err.Error()))
	}
	for _, r := range s.runs {
		if r.workspace == req.Workspace && r.live() {
			return s.remember(req, fail(runBusy, "run "+r.id+" is still running in "+req.Workspace))
		}
	}
	home, _ := os.UserHomeDir()
	ws := s.workspaces[req.Workspace]
	if ws == nil {
		// The one way a remote request creates a workspace: browsers only
		// ever attach to existing ones.
		if ws, err = s.createWorkspace(req.Workspace, home, runCols, runRows); err != nil {
			return s.remember(req, fail(runSpawnError, err.Error()))
		}
	}
	r := &remoteRun{id: req.RunID, workspace: ws.name, kind: req.Kind, at: time.Now()}
	t, err := s.newTabWith(ws, req.TabName, []string{path}, home, env, func(p *Pane) { p.run, r.pane = r, p })
	if err != nil {
		r.pane = nil
		return s.remember(req, fail(runSpawnError, err.Error()))
	}
	r.tabID = t.id
	r.report = remote.RunReport{Type: remote.RunStarted, RunID: r.id, TabID: strconv.FormatUint(uint64(t.id), 10)}
	// The new run replaces the workspace's finished run tabs. After the new
	// tab exists, so the workspace never empties and closes meanwhile.
	for _, old := range s.runs {
		if old.workspace == ws.name && old.finished && old.pane != nil && !old.pane.exited {
			s.paneExited(old.pane)
		}
	}
	s.runs[r.id] = r
	s.broadcastView(ws)
	s.broadcastStates()
	log.Printf("run %s: started %s (%s) in %s, tab %d", r.id, req.Kind, path, ws.name, t.id)
	return r.report
}

// remember keeps a failed run's report so a repeated Run gets the same answer.
func (s *Server) remember(req remote.RunRequest, rep remote.RunReport) remote.RunReport {
	if runIDRe.MatchString(req.RunID) {
		s.runs[req.RunID] = &remoteRun{id: req.RunID, workspace: req.Workspace, kind: req.Kind, finished: true, report: rep, at: time.Now()}
	}
	return rep
}

func (s *Server) pruneRuns() {
	for id, r := range s.runs {
		if !r.live() && (time.Since(r.at) > runKeep || len(s.runs) > runKeepMax) {
			delete(s.runs, id)
		}
	}
}

// runFinished reports a run's exit and keeps its pane on screen, unless
// the pane was closed or its workspace killed. Called with s.mu held.
func (s *Server) runFinished(p *Pane, code int) {
	r := p.run
	r.finished = true
	r.at = time.Now()
	r.report = remote.RunReport{Type: remote.RunExited, RunID: r.id, ExitCode: &code}
	log.Printf("run %s: exited with %d", r.id, code)
	s.remote.Report(r.report) // only queues, so safe under s.mu
	if p.exited || p.closing {
		return
	}
	p.finished = true
	note := fmt.Sprintf("\r\n\x1b[0m\x1b[2m[run finished, exit code %d]\x1b[0m\r\n", code)
	_, _ = p.emu.WriteString(note)
	if p.visible() {
		s.queueOutput(p, []byte(note))
	}
}

// Stop ends a run's process tree. A run that already finished, or that
// this daemon doesn't know, is left alone.
func (s *Server) Stop(runID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.runs[runID]
	if r == nil || !r.live() {
		return
	}
	log.Printf("run %s: stopping", runID)
	_ = r.pane.pty.KillTree(stopGrace)
}

// exitCode is a process's exit status, or -1 when it was killed by a signal
// or couldn't be waited for.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}
