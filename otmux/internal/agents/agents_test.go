package agents

import "testing"

func TestDetect(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"claude"}, "claude"},
		{[]string{`C:\Users\me\.local\bin\claude.exe`, "--resume"}, "claude"},
		{[]string{"node", `C:\npm\node_modules\@anthropic-ai\claude-code\cli.js`}, "claude"},
		{[]string{"/usr/bin/node", "--no-warnings", "/usr/lib/node_modules/@openai/codex/bin/codex.js"}, "codex"},
		{[]string{"python3", "-m", "aider"}, "aider"},
		{[]string{"/home/me/.local/bin/aider", "--model", "x"}, "aider"},
		{[]string{"gemini"}, "gemini"},
		{[]string{"cursor-agent"}, "cursor"},
		{[]string{"vim", "claude.md"}, ""},
		{[]string{"node", "server.js", "claude"}, ""},
		{[]string{"pwsh.exe", "-NoLogo"}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := Detect(c.args); got != c.want {
			t.Errorf("Detect(%q) = %q, want %q", c.args, got, c.want)
		}
	}
}
