package chrome

import "os/exec"

// execLookPath is split out so future tests can stub binary lookup without
// pulling os/exec into them.
var execLookPath = exec.LookPath
