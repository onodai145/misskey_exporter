package main

import (
	"runtime/debug"
	"strings"

	"github.com/prometheus/common/version"
)

// version.Version is only set by -ldflags, so a plain "go build" or
// "go install" would report no version. Fall back to the module version that
// the Go toolchain records from the git tag (or a pseudo-version without one).
func init() {
	if version.Version != "" {
		return
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		version.Version = moduleVersion(info.Main.Version)
	}
}

// moduleVersion converts a Go module version such as "v0.2.0" to "0.2.0". It
// returns "" for builds that have no version, which the toolchain reports as
// "(devel)".
func moduleVersion(v string) string {
	if v == "(devel)" {
		return ""
	}
	return strings.TrimPrefix(v, "v")
}
