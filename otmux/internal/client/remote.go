package client

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/keys"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/platform"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/protocol"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/remote"
)

// remoteLink is Settings › Remote › Connect account while it runs: the code
// to approve in the browser, then the wait for approval.
type remoteLink struct {
	cancel   context.CancelFunc
	code     string
	url      string
	opened   bool // the browser was opened at url
	starting bool // still asking the server for a code
}

func remoteState(st *protocol.RemoteStatus) string {
	if st == nil {
		return ""
	}
	return st.State + st.Error
}

// loadRemote rereads remote.json. Called with c.mu held.
func (c *Client) loadRemote() {
	a, err := remote.Load()
	if err != nil {
		c.remoteNote = "Couldn't read " + remote.Path() + ": " + err.Error()
	}
	c.remoteAcct = a
}

// reloadDaemonRemote tells the daemon remote.json changed.
func (c *Client) reloadDaemonRemote() {
	go func() { _ = c.conn.Send(protocol.TypeCommand, protocol.Command{Action: protocol.ActionRemoteReload}) }()
}

// startLink runs the device-code link in the background. Called with c.mu
// held.
func (c *Client) startLink() {
	if c.remoteLink != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	l := &remoteLink{cancel: cancel, starting: true}
	c.remoteLink, c.remoteNote = l, ""
	go func() {
		defer cancel()
		link, err := remote.StartLink(ctx)
		if err == nil {
			opened := platform.CanOpenBrowser() && platform.OpenURL(link.URL) == nil
			c.mu.Lock()
			l.starting, l.code, l.url, l.opened = false, link.UserCode, link.URL, opened
			c.mu.Unlock()
			c.markDirty()
			_, err = link.Wait(ctx)
		}
		c.mu.Lock()
		defer c.markDirty()
		defer c.mu.Unlock()
		if c.remoteLink != l {
			return // cancelled
		}
		c.remoteLink = nil
		c.loadRemote()
		c.reloadDaemonRemote() // clears a "stopped" status left by a revoke
		switch {
		case errors.Is(err, context.Canceled):
		case err != nil:
			c.remoteNote = "Not linked: " + err.Error()
		default:
			c.remoteNote = "Linked to " + c.remoteAcct.Email + ". Turn on Remote mode to reach this machine from the browser."
			if c.settings != nil {
				c.settings.sel[secRemote] = 0 // the Remote mode toggle
			}
		}
	}()
}

// cancelLink stops a link in progress. Called with c.mu held.
func (c *Client) cancelLink() {
	if c.remoteLink != nil {
		c.remoteLink.cancel()
		c.remoteLink = nil
	}
}

// updateRemote changes remote.json and tells the daemon. Called with c.mu
// held.
func (c *Client) updateRemote(fn func(*remote.Account)) bool {
	a, err := remote.Update(fn)
	if err != nil {
		c.remoteNote = "Couldn't save " + remote.Path() + ": " + err.Error()
		return false
	}
	c.remoteAcct = a
	c.reloadDaemonRemote()
	return true
}

// remoteRows are the selectable lines of Settings › Remote. Called with
// c.mu held.
func (c *Client) remoteRows() []settingsRow {
	a := c.remoteAcct
	if !a.Linked() {
		if c.remoteLink != nil {
			return []settingsRow{{left: "Cancel", run: func(c *Client) (keys.Action, bool) {
				c.cancelLink()
				return keys.Action{}, false
			}}}
		}
		rows := []settingsRow{{left: "Connect account…", right: hostOf(remote.ServerURL()), run: func(c *Client) (keys.Action, bool) {
			c.startLink()
			return keys.Action{}, false
		}}}
		if a.Notice != "" {
			rows = append(rows, settingsRow{left: "Dismiss", run: func(c *Client) (keys.Action, bool) {
				c.updateRemote(func(a *remote.Account) { a.Notice = "" }) // the daemon's reload clears the badge
				c.settings.sel[secRemote] = 0
				return keys.Action{}, false
			}})
		}
		return rows
	}
	mode := "off"
	if a.Enabled {
		mode = "on"
	}
	return []settingsRow{
		{left: "Remote mode", right: mode, run: func(c *Client) (keys.Action, bool) {
			on := !c.remoteAcct.Enabled
			if c.updateRemote(func(a *remote.Account) { a.Enabled = on }) {
				c.remoteNote = ""
			}
			return keys.Action{}, false
		}},
		{left: "Disconnect account", run: func(c *Client) (keys.Action, bool) {
			if c.updateRemote(func(a *remote.Account) { a.Token, a.Email, a.Enabled, a.Notice = "", "", false, "" }) {
				c.remoteNote = "Disconnected. The machine stays in the portal's device list until you remove it there."
				c.settings.sel[secRemote] = 0
			}
			return keys.Action{}, false
		}},
	}
}

// remoteInfo is the text above Settings › Remote's rows. Called with c.mu
// held.
func (c *Client) remoteInfo() []string {
	a := c.remoteAcct
	lines := []string{
		"Open this machine's workspaces from a browser, on desktop or phone.",
		"The machine connects out to the server; no ports to open.",
		"",
	}
	if !a.Linked() {
		lines = append(lines, "Account   not linked")
		if a.Notice != "" {
			lines = append(lines, "", a.Notice)
		}
		if l := c.remoteLink; l != nil {
			if l.starting {
				lines = append(lines, "", "Asking "+hostOf(remote.ServerURL())+" for a code…")
			} else {
				how := "Open this page in a browser and approve this machine:"
				if l.opened {
					how = "Approve this machine in the browser that just opened:"
				}
				lines = append(lines, "", how, l.url, "Code      "+l.code+"  (check it matches the page)", "Waiting for approval…")
			}
		}
	} else {
		lines = append(lines, "Account   "+a.Email, "Server    "+hostOf(a.Server))
		lines = append(lines, "Status    "+c.remoteStatusText())
		lines = append(lines, "", "Anyone signed in to this account can use the shells on this machine.")
	}
	if c.remoteNote != "" {
		lines = append(lines, "", c.remoteNote)
	}
	return lines
}

// remoteStatusText describes the daemon's connection. Called with c.mu held.
func (c *Client) remoteStatusText() string {
	st := c.state.Remote
	switch {
	case !c.remoteAcct.Enabled:
		return "remote mode off"
	case st == nil:
		return "the background service is older than remote mode; run `otmux restart`"
	}
	switch st.State {
	case protocol.RemoteOnline:
		switch st.Sessions {
		case 0:
			return "online"
		case 1:
			return "online · 1 browser attached"
		default:
			return fmt.Sprintf("online · %d browsers attached", st.Sessions)
		}
	case protocol.RemoteConnecting:
		return "connecting…"
	case protocol.RemoteRetrying:
		return "offline, retrying (" + st.Error + ")"
	case protocol.RemoteStopped:
		return "stopped: " + st.Error
	}
	return st.State
}

// remoteBadge is the status bar note while browsers are attached or remote
// mode stopped by itself; empty otherwise. Called with c.mu held.
func (c *Client) remoteBadge() (string, bool) {
	st := c.state.Remote
	switch {
	case st == nil:
		return "", false
	case st.Sessions == 1:
		return "◉ 1 browser", false
	case st.Sessions > 1:
		return fmt.Sprintf("◉ %d browsers", st.Sessions), false
	case st.State == protocol.RemoteStopped:
		return "remote stopped", true
	}
	return "", false
}

func hostOf(server string) string {
	if u, err := url.Parse(server); err == nil && u.Host != "" {
		return u.Host
	}
	return server
}
