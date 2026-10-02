package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/version"
)

// Linking failures the user can act on.
var (
	ErrDenied  = errors.New("the link was denied in the browser")
	ErrExpired = errors.New("the code expired; try again")
)

// meta describes this machine for the portal's device list. Names aren't
// unique and may change; the device id is the identity.
type meta struct {
	DeviceID     string `json:"device_id"`
	Name         string `json:"name"`
	Hostname     string `json:"hostname"`
	OS           string `json:"os"`
	OtmuxVersion string `json:"otmux_version"`
}

func machine(deviceID string) meta {
	host, _ := os.Hostname()
	return meta{DeviceID: deviceID, Name: host, Hostname: host, OS: runtime.GOOS, OtmuxVersion: version.Version}
}

// Link is a device-code link in progress (RFC 8628): the user approves
// UserCode at URL in a browser while Wait polls for the token.
type Link struct {
	UserCode string
	URL      string // the approval page, with the code filled in
	Expires  time.Time

	server     string
	deviceID   string
	deviceCode string
	interval   time.Duration
}

var httpClient = &http.Client{Timeout: 20 * time.Second}

// postJSON posts in and decodes the reply into out, whatever its status.
func postJSON(ctx context.Context, url string, in, out any) (int, error) {
	body, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return resp.StatusCode, err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return resp.StatusCode, fmt.Errorf("unexpected reply from the server (HTTP %d)", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

// StartLink asks the server for a code to approve. It makes this machine's
// device id on first use.
func StartLink(ctx context.Context) (*Link, error) {
	server := ServerURL()
	if _, err := checkServer(server); err != nil {
		return nil, err
	}
	a, err := Load()
	if err != nil {
		return nil, err
	}
	if a.DeviceID == "" {
		a.DeviceID = newDeviceID()
		if err := a.Save(); err != nil {
			return nil, err
		}
	}
	var reply struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
		URL        string `json:"verification_uri_complete"`
		ExpiresIn  int    `json:"expires_in"`
		Interval   int    `json:"interval"`
		Message    string `json:"message"`
	}
	status, err := postJSON(ctx, server+"/api/device/code", machine(a.DeviceID), &reply)
	switch {
	case err != nil:
		return nil, fmt.Errorf("couldn't reach %s: %w", server, err)
	case status == http.StatusTooManyRequests:
		return nil, errors.New("too many link attempts; wait a few minutes")
	case status != http.StatusOK || reply.DeviceCode == "":
		return nil, fmt.Errorf("the server refused (HTTP %d) %s", status, reply.Message)
	}
	return &Link{
		UserCode:   reply.UserCode,
		URL:        reply.URL,
		Expires:    time.Now().Add(time.Duration(reply.ExpiresIn) * time.Second),
		server:     server,
		deviceID:   a.DeviceID,
		deviceCode: reply.DeviceCode,
		interval:   time.Duration(max(reply.Interval, 1)) * time.Second,
	}, nil
}

// Wait polls until the user approves or denies the link, the code expires,
// or ctx ends. On approval it saves the token and returns the account.
// Remote mode stays as it was: linking alone never starts it.
func (l *Link) Wait(ctx context.Context) (Account, error) {
	interval := l.interval
	for {
		select {
		case <-ctx.Done():
			return Account{}, ctx.Err()
		case <-time.After(interval):
		}
		var reply struct {
			Error    string `json:"error"`
			Token    string `json:"device_token"`
			DeviceID string `json:"device_id"`
			Email    string `json:"account_email"`
		}
		status, err := postJSON(ctx, l.server+"/api/device/token", map[string]string{"device_code": l.deviceCode}, &reply)
		if err != nil {
			if ctx.Err() != nil {
				return Account{}, ctx.Err()
			}
			continue // a network blip; the code is still good until it expires
		}
		if status == http.StatusOK && reply.Token != "" {
			return Update(func(a *Account) {
				a.DeviceID, a.Server, a.Token, a.Email, a.Notice = l.deviceID, l.server, reply.Token, reply.Email, ""
			})
		}
		switch reply.Error {
		case "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
		case "access_denied":
			return Account{}, ErrDenied
		case "expired_token":
			return Account{}, ErrExpired
		default:
			return Account{}, fmt.Errorf("the server refused (HTTP %d) %s", status, reply.Error)
		}
	}
}
