// Package version reports the otmux build version.
package version

// Version is the otmux release. Release builds also stamp it with
// -ldflags "-X .../version.Version=..." from the git tag (otmux/vX.Y.Z).
var Version = "1.0.0"
