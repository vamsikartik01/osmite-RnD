package client

import (
	"strings"
	"testing"
)

func TestLauncherLines(t *testing.T) {
	c := &Client{}
	if got := strings.Join(c.launcherLines(), "\n"); !strings.Contains(got, "none: the server can't start runs") {
		t.Fatalf("no launchers:\n%s", got)
	}
	c.launchers = map[string]string{"offspace-agent": "/opt/offspace/launch-agent", "b": "/usr/local/bin/b"}
	lines := c.launcherLines()
	if len(lines) != 4 || !strings.HasPrefix(strings.TrimSpace(lines[2]), "b ") || !strings.Contains(lines[3], "offspace-agent  /opt/offspace/launch-agent") {
		t.Fatalf("%q", lines)
	}
}
