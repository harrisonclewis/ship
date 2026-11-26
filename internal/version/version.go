// Package version records the ship version stamped into the binary at build time.
package version

// Version is the release version, without a leading "v". Builds from a source
// checkout default to "dev"; make build sets it with
// -ldflags "-X github.com/harrisonclewis/ship/internal/version.Version=...".
var Version = "dev"
