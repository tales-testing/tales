package version

import (
	"fmt"
	"runtime"
	"sync"
)

var version string
var gitCommit string
var gitTreeState string
var buildDate string

// Info is build/runtime metadata for the Tales binary.
type Info struct {
	Version      string
	GitCommit    string
	GitTreeState string
	BuildDate    string
	GoVersion    string
	Compiler     string
	Platform     string
}

var instance *Info
var instanceOnce sync.Once

// Get returns immutable build/runtime metadata.
func Get() *Info {
	instanceOnce.Do(func() {
		instance = &Info{
			Version:      version,
			GitCommit:    gitCommit,
			GitTreeState: gitTreeState,
			BuildDate:    buildDate,
			GoVersion:    runtime.Version(),
			Compiler:     runtime.Compiler,
			Platform:     fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		}
	})

	return instance
}
