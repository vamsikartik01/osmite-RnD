package update

import (
	"regexp"
	"strings"
)

// Release is one version's entry in the changelog.
type Release struct {
	Version string   // e.g. "1.3.2", or "Unreleased"
	Items   []string // its bullet points, as plain text
}

var (
	heading = regexp.MustCompile(`^## \[([^\]]+)\]`)
	mdLink  = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
)

// Notes returns what changed after version since, up to and including upto,
// newest first, from a Keep a Changelog file. A build that isn't a release
// (upto like "1.3.2-dev") also gets the Unreleased entry. An unknown since
// gives just the newest entry.
func Notes(changelog, since, upto string) []Release {
	_, release := parse(upto)
	_, known := parse(since)
	var out []Release
	var cur *Release
	for _, line := range strings.Split(changelog, "\n") {
		line = strings.TrimRight(line, "\r")
		if m := heading.FindStringSubmatch(line); m != nil {
			cur = nil
			v := m[1]
			want := v == "Unreleased" && !release
			if v != "Unreleased" {
				want = (!release || !Newer(v, upto)) && (!known || Newer(v, since))
			}
			if want {
				out = append(out, Release{Version: v})
				cur = &out[len(out)-1]
			}
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case strings.HasPrefix(line, "- "):
			cur.Items = append(cur.Items, plain(line[2:]))
		case strings.HasPrefix(strings.TrimSpace(line), "- "):
			cur.Items = append(cur.Items, plain(strings.TrimSpace(line)[2:]))
		case strings.HasPrefix(line, "  ") && len(cur.Items) > 0 && strings.TrimSpace(line) != "":
			cur.Items[len(cur.Items)-1] += " " + plain(strings.TrimSpace(line))
		}
	}
	// Drop empty entries (e.g. an Unreleased with nothing in it yet).
	kept := out[:0]
	for _, r := range out {
		if len(r.Items) > 0 {
			kept = append(kept, r)
		}
	}
	if !known && len(kept) > 1 {
		kept = kept[:1]
	}
	return kept
}

// plain strips the Markdown a terminal can't show: links, code and bold.
func plain(s string) string {
	s = mdLink.ReplaceAllString(s, "$1")
	return strings.NewReplacer("`", "", "**", "").Replace(s)
}
