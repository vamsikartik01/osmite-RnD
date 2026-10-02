// Package agents recognises AI coding agents (Claude Code, Codex, Gemini
// CLI, ...) from a process's arguments, so otmux can pin the tabs they run in.
package agents

import (
	"path/filepath"
	"strings"
)

// known maps a program or package name to the agent it belongs to.
var known = map[string]string{
	"claude": "claude", "claude-code": "claude",
	"codex":  "codex",
	"gemini": "gemini", "gemini-cli": "gemini",
	"aider":    "aider",
	"opencode": "opencode", "opencode-ai": "opencode",
	"cursor-agent": "cursor",
	"goose":        "goose",
	"amp":          "amp",
	"qwen":         "qwen", "qwen-code": "qwen",
	"crush": "crush",
}

// names are how agents are shown to people.
var names = map[string]string{
	"claude": "Claude", "codex": "Codex", "gemini": "Gemini", "aider": "Aider",
	"opencode": "opencode", "cursor": "Cursor", "goose": "Goose", "amp": "Amp",
	"qwen": "Qwen", "crush": "Crush",
}

// Name is agent's display name, e.g. "Claude" for "claude".
func Name(agent string) string {
	if n := names[agent]; n != "" {
		return n
	}
	return agent
}

// interpreters run agents distributed as scripts (npm, pip).
var interpreters = map[string]bool{
	"node": true, "bun": true, "deno": true, "npx": true,
	"python": true, "python3": true, "pythonw": true, "uv": true, "uvx": true, "pipx": true,
}

// Detect returns the agent args runs, or "". Only the program itself is
// matched, plus the script path when the program is an interpreter, so
// `vim claude.md` doesn't count.
func Detect(args []string) string {
	if len(args) == 0 {
		return ""
	}
	prog := base(args[0])
	if a := known[prog]; a != "" {
		return a
	}
	if !interpreters[prog] {
		return ""
	}
	for _, arg := range args[1:] {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		// Script path like .../node_modules/@anthropic-ai/claude-code/cli.js:
		// check every path segment.
		for _, seg := range strings.FieldsFunc(arg, func(r rune) bool { return r == '/' || r == '\\' }) {
			if a := known[base(seg)]; a != "" {
				return a
			}
		}
		return "" // only the first non-flag argument is the script
	}
	return ""
}

// base is a lowercase file name without directory or extension.
func base(p string) string {
	p = strings.ToLower(filepath.Base(strings.ReplaceAll(p, `\`, "/")))
	return strings.TrimSuffix(p, filepath.Ext(p))
}
