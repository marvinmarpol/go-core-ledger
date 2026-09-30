package ledger

import "time"

// Clock is injected by callers; never call time.Now() directly in domain or posting code.
type Clock interface {
	Now() time.Time
}
