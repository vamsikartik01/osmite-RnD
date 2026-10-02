package client

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/platform"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/update"
	"github.com/vamsikartik01/osmite-RnD/otmux/internal/version"
)

// updateDir holds update.json, next to the daemon's socket and log.
func updateDir() string { return filepath.Dir(platform.LogPath()) }

// startUpdates runs at attach: it tidies up after earlier updates, notes an
// update that's waiting for a restart, and checks for a new version in the
// background if automatic updates are on and a day has passed.
func (c *Client) startUpdates(daemonVersion string) {
	if exe, err := update.Executable(); err == nil {
		update.Cleanup(exe)
	}
	st := update.LoadState(updateDir())
	c.mu.Lock()
	c.updateInfo = st
	switch {
	case daemonVersion != "" && daemonVersion != version.Version:
		// The program was updated but the running daemon is still the old one.
		c.updateBadge = fmt.Sprintf("updated to %s · restart to finish", version.Version)
		c.updateTarget, c.updateSince = version.Version, daemonVersion
		c.updateStatus = fmt.Sprintf("otmux %s is installed, but the background service is still %s. "+
			"Run `otmux restart` to finish (this closes running shells).", version.Version, daemonVersion)
	case st.Installed != "" && update.Newer(st.Installed, version.Version):
		c.updateBadge = fmt.Sprintf("%s installed · restart to finish", st.Installed)
		c.updateTarget, c.updateSince = st.Installed, version.Version
		c.updateStatus = fmt.Sprintf("otmux %s is installed and starts next time you run otmux.", st.Installed)
	}
	auto := c.cfg.AutoUpdateOn() && !update.Disabled() && st.Due(time.Now())
	c.mu.Unlock()
	if auto {
		go c.checkUpdates(false)
	}
}

// checkUpdates looks for a new version and installs it. A manual check
// (Update now) reports every outcome; an automatic one stays quiet unless
// it installed something.
func (c *Client) checkUpdates(manual bool) {
	c.mu.Lock()
	if c.updateBusy {
		c.mu.Unlock()
		return
	}
	c.updateBusy = true
	if manual {
		c.updateStatus = "Checking for updates…"
	}
	c.mu.Unlock()
	c.markDirty()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	exe, err := update.Executable()
	var r update.Result
	if err == nil {
		r, err = update.Run(ctx, exe, version.Version)
	}

	c.mu.Lock()
	defer c.markDirty()
	defer c.mu.Unlock()
	c.updateBusy = false
	st := c.updateInfo
	if err == nil || r.Latest != "" {
		st.LastCheck = time.Now()
		st.Latest = r.Latest
	}
	switch {
	case err != nil && errors.Is(err, update.ErrNotManaged):
		if manual {
			c.updateStatus = "This copy can't update itself (" + err.Error() + "). Update it the way you installed it."
		}
	case err != nil:
		if manual {
			c.updateStatus = "Couldn't update: " + err.Error()
		}
	case r.Installed:
		st.Installed = r.Latest
		c.updateBadge = fmt.Sprintf("%s installed · restart to finish", r.Latest)
		c.updateTarget, c.updateSince = r.Latest, version.Version
		c.updateStatus = fmt.Sprintf("otmux %s is installed. It starts next time you run otmux; "+
			"run `otmux restart` to restart the background service too (this closes running shells).", r.Latest)
	default:
		if manual {
			c.updateStatus = fmt.Sprintf("otmux %s is the latest version.", version.Version)
		}
	}
	c.updateInfo = st
	_ = st.Save(updateDir())
}

// updateInfoLines describes the update state for Settings › Updates.
// Called with c.mu held.
func (c *Client) updateInfoLines() []string {
	st := c.updateInfo
	checked := "not checked yet"
	if !st.LastCheck.IsZero() {
		checked = "checked " + st.LastCheck.Format("Jan 2 15:04")
	}
	latest := "Latest     unknown (" + checked + ")"
	if st.Latest != "" {
		latest = "Latest     " + st.Latest + " (" + checked + ")"
	}
	lines := []string{
		"Installed  " + version.Version,
		latest,
		"",
		"Updates come from GitHub releases, are verified with SHA-256,",
		"and never restart running shells.",
	}
	if c.updateStatus != "" {
		lines = append(lines, "", c.updateStatus)
	}
	return lines
}
