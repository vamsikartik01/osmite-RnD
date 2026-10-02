// Package version reports the otmux build version.
package version

// Version is the otmux release. Release builds also stamp it with
// -ldflags "-X .../version.Version=..." from the git tag (otmux/vX.Y.Z).
var Version = "1.1.0"

// Release is "true" in official release builds (set by release.ps1), and
// empty when built from source with plain `go build`. Only release builds
// update themselves, so a copy you built yourself is never replaced.
var Release = ""
