package update

import (
	"reflect"
	"testing"
)

const changelog = `# Changelog

## [Unreleased]

### Fixed

- A dev fix.

## [1.3.2] - 2026-10-03

### Changed

- Agents at their prompt show as idle, and the ` + "`otmux restart`" + `
  command explains itself.
  - A nested point.

## [1.3.1] - 2026-10-02

- Smoother screens, see [the docs](https://example.com).

## [1.3.0] - 2026-10-01

- **Remote** mode.
`

func TestNotes(t *testing.T) {
	cases := []struct {
		since, upto string
		want        []Release
	}{
		{"1.3.0", "1.3.2", []Release{
			{"1.3.2", []string{"Agents at their prompt show as idle, and the otmux restart command explains itself.", "A nested point."}},
			{"1.3.1", []string{"Smoother screens, see the docs."}},
		}},
		{"1.3.1", "1.3.1", nil},
		{"1.3.1", "1.3.2-dev", []Release{
			{"Unreleased", []string{"A dev fix."}},
			{"1.3.2", []string{"Agents at their prompt show as idle, and the otmux restart command explains itself.", "A nested point."}},
		}},
		{"dev", "1.3.0", []Release{{"1.3.0", []string{"Remote mode."}}}},
	}
	for _, c := range cases {
		if got := Notes(changelog, c.since, c.upto); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Notes(%q, %q) = %#v, want %#v", c.since, c.upto, got, c.want)
		}
	}
}
