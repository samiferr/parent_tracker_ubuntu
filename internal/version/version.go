// Package version holds the build version, set at link time.
package version

// Version is overridden with -ldflags "-X .../internal/version.Version=1.2.3".
var Version = "0.1.0-dev"
