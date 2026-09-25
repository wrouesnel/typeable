// Package version exposes build version information. Version is set at link
// time by the build system via -ldflags "-X".
package version

// Name is the overall name of the application at the Git repository level.
const Name = "typeable"

// Description is an overall description of its function.
const Description = `Encode data as grids of text for humans to retype, and decode them with error correction`

//nolint:gochecknoglobals // overridden at link time
var Version = "v0.0.0-dev"
