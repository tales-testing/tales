package driver

import "errors"

// ErrNavigated reports that the page committed a navigation while a call
// was still waiting on the document that navigation replaced. The call
// did not complete; it can be retried against the new document. A driver
// returns it (possibly wrapped) when it can tell that a reply will never
// come, instead of letting the caller wait for its deadline.
var ErrNavigated = errors.New("page navigated during the call")
