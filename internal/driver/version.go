package driver

import (
	"runtime"
	"runtime/debug"
)

// Version is the version of ki. It travels in the binary because
// debug.ReadBuildInfo has no stable number to offer: it reports "(devel)" for a
// go build without VCS information, and a pseudo-version that changes with
// every commit for a module at the root of a git repository.
const Version = "0.0.1"

// goVersion returns the version of Go that built this binary, such as
// "go1.27.3". debug.ReadBuildInfo fills GoVersion from runtime.Version(), so
// both paths give the same string; the fallback only covers a binary that
// carries no build information.
func goVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		return bi.GoVersion
	}
	return runtime.Version()
}
