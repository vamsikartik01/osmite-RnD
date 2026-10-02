// Package otmux holds files built into the otmux program.
package otmux

import _ "embed"

// Changelog is CHANGELOG.md, so a newly installed otmux can say what changed.
//
//go:embed CHANGELOG.md
var Changelog string
