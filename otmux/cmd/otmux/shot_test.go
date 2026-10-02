package main

import (
	"fmt"
	"html"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
)

// TestScreenshots renders otmux in a few typical states to HTML files (and
// PNGs, when Edge or Chrome is around) for looking at the design. It only
// runs when OTMUX_SHOTS names an output folder:
//
//	OTMUX_SHOTS=shots go test -run TestScreenshots ./cmd/otmux
func TestScreenshots(t *testing.T) {
	out := os.Getenv("OTMUX_SHOTS")
	if out == "" {
		t.Skip("set OTMUX_SHOTS to a folder to render screenshots")
	}
	out, _ = filepath.Abs(out)
	_ = os.MkdirAll(out, 0o755)
	dir := shortTempDir(t)
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	bin := filepath.Join(dir, "otmux"+exe)
	if b, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, b)
	}
	// A stand-in coding agent, so the watch list has something in it.
	agentDir := filepath.Join(dir, "agent")
	_ = os.MkdirAll(agentDir, 0o700)
	_ = os.WriteFile(filepath.Join(agentDir, "main.go"), []byte(fakeAgent), 0o600)
	agent := filepath.Join(dir, "bin", "claude"+exe)
	build := exec.Command("go", "build", "-o", agent, "main.go")
	build.Dir, build.Env = agentDir, append(os.Environ(), "GO111MODULE=off")
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build agent: %v\n%s", err, b)
	}

	shell := "/bin/sh"
	if runtime.GOOS == "windows" {
		shell = "cmd.exe"
	}
	cfg := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfg, []byte(`{"workspaces":[{"name":"docs","path":"`+strings.ReplaceAll(dir, `\`, `\\`)+`"}]}`), 0o600)
	env := []string{
		"OTMUX_SOCKET=" + filepath.Join(dir, "s.sock"), "OTMUX_SHELL=" + shell, "OTMUX_PANE=",
		"OTMUX_CONFIG=" + cfg, "OTMUX_PREFIX=", "OTMUX_NO_UPDATE=1",
		"PATH=" + filepath.Dir(agent) + string(os.PathListSeparator) + os.Getenv("PATH"),
	}
	t.Cleanup(func() { runOtmux(t, env, bin, "kill-server") })

	tm := startTermArgs(t, bin, []string{"new", "osmite"}, env, 120, 32)
	shot := func(name string) {
		time.Sleep(400 * time.Millisecond)
		tm.mu.Lock()
		page := screenHTML(tm.emu.Width(), tm.emu.Height(), tm.emu.CellAt)
		tm.mu.Unlock()
		path := filepath.Join(out, name+".html")
		if err := os.WriteFile(path, []byte(page), 0o644); err != nil {
			t.Fatal(err)
		}
		toPNG(t, path, tm.emu.Width(), tm.emu.Height())
	}
	tm.waitFor(t, "splash", func(s string) bool { return strings.Contains(s, "from osmite RnD") })
	shot("1-splash")

	keys := func(s string) {
		tm.typ(s)
		time.Sleep(700 * time.Millisecond)
	}
	keys("echo hello from otmux\r")
	keys("\x02v")
	time.Sleep(time.Second) // the new shell starting
	keys("claude\r")
	keys("\x02h")
	time.Sleep(time.Second)
	keys("cd ..\r")
	keys("\x02c")
	keys("api\r")
	time.Sleep(time.Second)
	keys("dir /w\r")
	time.Sleep(3 * time.Second) // agent scan
	keys("\x020")
	keys("\x1b[<0;50;12M\x1b[<0;50;12m") // click the left pane
	shot("2-split-agent")

	keys("\x02")
	shot("3-prefix")
	keys(" ") // the prefix is pending: Space opens the palette
	shot("4-palette")
	keys("settings")
	keys("\r")
	shot("5-settings")
	keys("\x1b[B")
	shot("6-settings-appearance")
}

const fakeAgent = `package main

import (
	"fmt"
	"time"
)

func main() {
	fmt.Print("\x1b]0;claude\x07")
	fmt.Println("\x1b[38;5;208m*\x1b[0m Welcome to \x1b[1mClaude Code\x1b[0m")
	fmt.Println()
	fmt.Println("  /help for help, /status for your current setup")
	fmt.Println()
	fmt.Println("> fix the resize bug in the sidebar")
	for i := 0; ; i++ {
		fmt.Printf("\r  Thinking%s   ", []string{".", "..", "..."}[i%3])
		time.Sleep(300 * time.Millisecond)
	}
}
`

// screenHTML renders a terminal screen as an HTML page.
func screenHTML(w, h int, at func(x, y int) *uv.Cell) string {
	css := func(c color.Color, def string) string {
		if c == nil {
			return def
		}
		r, g, b, _ := c.RGBA()
		return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
	}
	var sb strings.Builder
	sb.WriteString(`<!doctype html><meta charset="utf-8"><style>
body{margin:0;background:#0c0c0c}
pre{margin:0;font:15px/19px "Cascadia Mono","Consolas",monospace;color:#cccccc}
span{display:inline-block;height:19px;vertical-align:top}
</style><pre>`)
	for y := 0; y < h; y++ {
		for x := 0; x < w; {
			c := at(x, y)
			if c == nil {
				sb.WriteString(" ")
				x++
				continue
			}
			cw := max(c.Width, 1)
			content := c.Content
			if content == "" {
				content = " "
			}
			fg, bg := css(c.Style.Fg, "#cccccc"), css(c.Style.Bg, "#0c0c0c")
			if c.Style.Attrs&uv.AttrReverse != 0 {
				fg, bg = bg, fg
			}
			weight := ""
			if c.Style.Attrs&uv.AttrBold != 0 {
				weight = ";font-weight:bold"
			}
			fmt.Fprintf(&sb, `<span style="color:%s;background:%s;width:%dch%s">%s</span>`, fg, bg, cw, weight, html.EscapeString(content))
			x += cw
		}
		sb.WriteString("\n")
	}
	sb.WriteString("</pre>")
	return sb.String()
}

// toPNG screenshots an HTML page with a headless Edge or Chrome, if found.
func toPNG(t *testing.T, page string, cols, rows int) {
	var browser string
	for _, p := range []string{
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		"google-chrome", "chromium",
	} {
		if b, err := exec.LookPath(p); err == nil {
			browser = b
			break
		}
	}
	if browser == "" {
		return
	}
	png := strings.TrimSuffix(page, ".html") + ".png"
	cmd := exec.Command(browser, "--headless", "--disable-gpu", "--hide-scrollbars",
		"--screenshot="+png, fmt.Sprintf("--window-size=%d,%d", cols*88/10, rows*19),
		"file:///"+filepath.ToSlash(page))
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Logf("screenshot %s: %v\n%s", page, err, b)
	}
}
