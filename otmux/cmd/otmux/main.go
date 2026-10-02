// Command otmux is a terminal workspace: persistent workspaces, tabs and
// split panes that survive closing the terminal, driven with tmux-style keys
// or the mouse. Run `otmux help` for usage.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	xterm "github.com/charmbracelet/x/term"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/client"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/config"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/daemon"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/keys"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/platform"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/update"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/version"
)

const usage = `otmux - terminal workspaces

Usage:
  otmux                     attach to the "default" workspace, creating it if needed
  otmux new [name]          create a workspace and attach (name defaults to the folder)
  otmux attach <name>       attach to an existing workspace      (alias: a)
  otmux ls                  list workspaces                      (alias: list)
  otmux kill <name>         close a workspace and its shells
  otmux kill-server         stop the daemon and every shell
  otmux restart [-y]        stop the daemon and every shell, then open otmux
                            again (e.g. to start a new version); -y skips asking
  otmux keys                list key bindings
  otmux update              update otmux to the latest release
  otmux version

Inside otmux, press %[1]s then:
  v split side by side   h split stacked   arrows focus   shift+arrows resize
  z zoom   x close pane   c new tab   n/p next/prev tab   0-9 jump   r rename
  w workspaces   a new workspace   [ ] prev/next workspace
  space all commands   d detach
Mouse: click panes, tabs and workspaces; drag dividers; drag to select and
copy; right-click to paste; wheel scrolls back through history.

Environment:
  OTMUX_PREFIX   prefix key, e.g. ctrl+a (default ctrl+b)
  OTMUX_SHELL    shell for new panes
  OTMUX_SOCKET   daemon socket path (run an isolated daemon)
  OTMUX_REMOTE_URL  remote server to link with (default https://otmux.osmite.site)
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "otmux:", err)
		os.Exit(1)
	}
}

func prefix() string { return strings.ToLower(strings.TrimSpace(os.Getenv("OTMUX_PREFIX"))) }

func run(args []string) error {
	cmd := ""
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	arg := func() string {
		if len(args) > 0 {
			return args[0]
		}
		return ""
	}
	switch cmd {
	case "":
		return attach("default", protocol.AttachOrCreate)
	case "new", "n":
		return attach(arg(), protocol.CreateNew)
	case "attach", "a":
		if arg() == "" {
			return attach("default", protocol.AttachOrCreate)
		}
		return attach(arg(), protocol.AttachExisting)
	case "ls", "list":
		return list()
	case "kill":
		if arg() == "" {
			return errors.New("usage: otmux kill <workspace>")
		}
		return oneShot(protocol.Command{Action: protocol.ActionKillWorkspace, Arg: arg()})
	case "kill-server":
		return oneShot(protocol.Command{Action: protocol.ActionKillServer})
	case "restart":
		return restart(arg() == "-y" || arg() == "--yes")
	case "update":
		return selfUpdate()
	case "keys":
		printKeys()
		return nil
	case "daemon":
		return runDaemon()
	case "version", "--version", "-v":
		if version.Release == "true" {
			fmt.Println("otmux", version.Version)
		} else {
			fmt.Println("otmux", version.Version, "(built from source)")
		}
		return nil
	case "help", "-h", "--help":
		fmt.Printf(usage, keys.Label(keys.Default(prefix()).Prefix))
		return nil
	default:
		fmt.Fprintf(os.Stderr, usage, keys.Label(keys.Default(prefix()).Prefix))
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func attach(workspace, mode string) error {
	if os.Getenv("OTMUX_PANE") != "" {
		return errors.New("already inside otmux; use the workspace picker (prefix w) instead of nesting")
	}
	if err := ensureDaemon(); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "otmux: ignoring settings file %s: %v\n", config.Path(), err)
	}
	opts := client.Options{Workspace: workspace, Mode: mode, Prefix: prefix(), Config: cfg}
	if p, ok := cfg.Profile(workspace); ok {
		opts.Dir = p.Path // a saved workspace opens in its folder
	}
	reason, err := client.Attach(opts)
	if err != nil {
		return err
	}
	if reason != "" {
		fmt.Printf("[%s]\n", reason)
	}
	return nil
}

// selfUpdate installs the latest release over this program.
func selfUpdate() error {
	exe, err := update.Executable()
	if err != nil {
		return err
	}
	fmt.Println("Checking for updates...")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	r, err := update.Run(ctx, exe, version.Version)
	if err != nil {
		return err
	}
	if !r.Installed {
		fmt.Printf("otmux %s is the latest version.\n", version.Version)
		return nil
	}
	st := update.LoadState(filepath.Dir(platform.LogPath()))
	st.LastCheck, st.Latest, st.Installed = time.Now(), r.Latest, r.Latest
	_ = st.Save(filepath.Dir(platform.LogPath()))
	fmt.Printf("Updated otmux %s -> %s.\n", version.Version, r.Latest)
	fmt.Println("It starts next time you run otmux. To restart the background service too, run `otmux kill-server` (this closes running shells).")
	return nil
}

func printKeys() {
	km := keys.Default(prefix())
	p := keys.Label(km.Prefix)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintf(tw, "Press %s, then:\n", p)
	group := ""
	for _, cmd := range keys.Catalog {
		if cmd.Group != group {
			group = cmd.Group
			fmt.Fprintf(tw, "\n%s\n", group)
		}
		k := km.KeyFor(cmd.Action)
		if k == "" {
			k = "(command palette)"
		} else {
			k = keys.Label(k)
		}
		fmt.Fprintf(tw, "  %s\t%s\n", k, cmd.Title)
	}
	fmt.Fprintf(tw, "\nOther\n  0-9\tGo to tab\n  :  or  ?\tCommand palette\n  %s\tSend %s to the shell\n", p, p)
	_ = tw.Flush()
}

func dial() (*protocol.Conn, error) {
	nc, err := net.DialTimeout("unix", platform.SocketPath(), time.Second)
	if err != nil {
		return nil, err
	}
	return protocol.NewConn(nc), nil
}

// ensureDaemon starts a background daemon unless one is already listening.
func ensureDaemon() error {
	if c, err := dial(); err == nil {
		c.Close()
		return nil
	}
	if err := platform.EnsureRuntimeDir(); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logf, err := os.OpenFile(platform.LogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()

	cmd := exec.Command(exe, "daemon")
	cmd.Stdout, cmd.Stderr = logf, logf
	platform.Detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	_ = cmd.Process.Release()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := dial(); err == nil {
			c.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not start; see %s", platform.LogPath())
}

func runDaemon() error {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	err := daemon.Run()
	if errors.Is(err, daemon.ErrAlreadyRunning) {
		return fmt.Errorf("%w (socket %s)", err, platform.SocketPath())
	}
	return err
}

// restart stops the daemon, and with it every shell, then opens otmux
// again with a fresh daemon from this program, e.g. one just updated. It
// asks first when there are shells to lose, unless yes is set.
func restart(yes bool) error {
	if os.Getenv("OTMUX_PANE") != "" {
		return errors.New("can't restart from inside otmux: it would close this shell too; run it from a plain terminal")
	}
	if reply, err := workspaces(); err == nil && len(reply.Workspaces) > 0 && !yes {
		tabs := 0
		for _, w := range reply.Workspaces {
			tabs += w.Tabs
		}
		if !xterm.IsTerminal(os.Stdin.Fd()) {
			return errors.New("restarting closes every shell in otmux; run otmux restart -y to confirm")
		}
		fmt.Printf("Restarting closes %s in %d workspace(s), and the programs running in them. Continue? [y/N] ",
			tabsLabel(tabs), len(reply.Workspaces))
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			fmt.Println("Not restarted.")
			return nil
		}
	}
	if err := oneShot(protocol.Command{Action: protocol.ActionKillServer}); err != nil {
		return err
	}
	// Wait for the old daemon to let go of its socket before starting anew.
	deadline := time.Now().Add(5 * time.Second)
	for {
		c, err := dial()
		if err != nil {
			break
		}
		c.Close()
		if time.Now().After(deadline) {
			return errors.New("the daemon didn't stop; try otmux kill-server")
		}
		time.Sleep(50 * time.Millisecond)
	}
	return attach("default", protocol.AttachOrCreate)
}

// workspaces asks the daemon for its workspaces.
func workspaces() (protocol.ListReply, error) {
	var reply protocol.ListReply
	c, err := dial()
	if err != nil {
		return reply, err
	}
	defer c.Close()
	if err := c.SendEmpty(protocol.TypeList); err != nil {
		return reply, err
	}
	f, err := c.Read()
	if err != nil {
		return reply, err
	}
	err = f.Decode(&reply)
	return reply, err
}

func list() error {
	if _, err := dial(); err != nil {
		fmt.Println("no daemon running")
		return nil
	}
	reply, err := workspaces()
	if err != nil {
		return err
	}
	if len(reply.Workspaces) == 0 {
		fmt.Println("no workspaces")
	}
	for _, w := range reply.Workspaces {
		attached := ""
		if w.Clients > 0 {
			attached = fmt.Sprintf(" (%d attached)", w.Clients)
		}
		fmt.Printf("%s: %s%s\n", w.Name, tabsLabel(w.Tabs), attached)
	}
	return nil
}

// oneShot sends a single command to the daemon and reports its result.
func oneShot(cmd protocol.Command) error {
	c, err := dial()
	if err != nil {
		fmt.Println("no daemon running")
		return nil
	}
	defer c.Close()
	if err := c.Send(protocol.TypeCommand, cmd); err != nil {
		return err
	}
	f, err := c.Read()
	if err != nil {
		return nil // the daemon may exit before replying to kill-server
	}
	var w protocol.Welcome
	if f.Decode(&w) == nil && w.Error != "" {
		return errors.New(w.Error)
	}
	return nil
}

func tabsLabel(n int) string {
	if n == 1 {
		return "1 tab"
	}
	return fmt.Sprintf("%d tabs", n)
}
