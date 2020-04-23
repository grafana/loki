package ruler

import (
	"context"
)

// TransferOut is a noop for the ruler
func (r *Ruler) TransferOut(ctx context.Context) error {
	return nil
}

// Flush triggers a flush of all the work items currently
// scheduled by the ruler, currently every ruler will
// query a backend rule store for it's rules so no
// flush is required.
func (r *Ruler) Flush() {}
