// Package remote is otmux remote mode: linking this machine to an account
// on the otmux remote server, and the daemon's connection to that server,
// which lets browsers open this machine's workspaces.
//
// The account (device id and token) lives in remote.json next to the
// settings file, readable only by the user. The token is a secret: it is
// never logged, shown, or written anywhere else.
package remote

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/config"
)

// DefaultServer is the otmux remote server. OTMUX_REMOTE_URL overrides it
// when linking, e.g. for a local copy of the server.
const DefaultServer = "https://otmux.osmite.site"

// Account is this machine's link to a remote account.
type Account struct {
	// DeviceID identifies this machine. It is made on the first link and
	// kept when the account is disconnected, so relinking is the same device.
	DeviceID string `json:"device_id,omitempty"`
	// Server is where the token was issued; the daemon connects there.
	Server string `json:"server,omitempty"`
	Token  string `json:"device_token,omitempty"`
	Email  string `json:"account_email,omitempty"`
	// Enabled is the Remote mode setting: the daemon stays connected.
	Enabled bool `json:"enabled,omitempty"`
	// Notice says why remote mode stopped by itself, e.g. the device was
	// revoked from the portal. Cleared on the next link.
	Notice string `json:"notice,omitempty"`
}

// Linked reports whether the account holds a token.
func (a Account) Linked() bool { return a.Token != "" }

// Path is remote.json, next to the settings file.
func Path() string { return filepath.Join(filepath.Dir(config.Path()), "remote.json") }

// Load reads the account. A missing file is an empty account.
func Load() (Account, error) {
	var a Account
	data, err := os.ReadFile(Path())
	if errors.Is(err, fs.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return a, err
	}
	if err := json.Unmarshal(data, &a); err != nil {
		return Account{}, fmt.Errorf("%s: %w", Path(), err)
	}
	return a, nil
}

// Save writes the account, readable only by the user.
func (a Account) Save() error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	// Retry the rename briefly: on Windows it fails while another process
	// has the file open.
	for range 10 {
		if err = os.Rename(tmp, p); err == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = os.Remove(tmp)
	if werr := os.WriteFile(p, data, 0o600); werr != nil {
		return fmt.Errorf("%w (and writing in place: %v)", err, werr)
	}
	return nil
}

// Update loads the account, applies fn and saves it.
func Update(fn func(*Account)) (Account, error) {
	a, err := Load()
	if err != nil {
		return a, err
	}
	fn(&a)
	return a, a.Save()
}

// ServerURL is the server to link with: OTMUX_REMOTE_URL or DefaultServer.
func ServerURL() string {
	if u := strings.TrimSpace(os.Getenv("OTMUX_REMOTE_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	return DefaultServer
}

// checkServer refuses anything but https, except plain http to this
// machine for testing against a local server.
func checkServer(server string) (*url.URL, error) {
	u, err := url.Parse(server)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("bad server URL %q", server)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if h := u.Hostname(); h != "localhost" && h != "127.0.0.1" && h != "::1" {
			return nil, fmt.Errorf("server %s must use https", server)
		}
	default:
		return nil, fmt.Errorf("server %s must use https", server)
	}
	return u, nil
}

// newDeviceID returns a random UUID v4.
func newDeviceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
