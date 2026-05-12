// Package appledriver exposes the Swift/XCUITest driver source as an
// embedded filesystem so the Tales binary can extract and build it at
// runtime without requiring this repository on the user's machine.
package appledriver

import (
	"embed"
	"io/fs"
)

// SourceRoot is the directory name under which the driver source lives
// inside the embedded filesystem.
const SourceRoot = "TalesAppleDriver"

//go:embed all:TalesAppleDriver
var fsRoot embed.FS

// FS returns the embedded driver source. The relevant subtree is rooted
// at SourceRoot.
func FS() fs.FS {
	return fsRoot
}
