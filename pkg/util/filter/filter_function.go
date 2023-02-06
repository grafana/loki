package filter

import "time"

type Func func(ts time.Time, s string) bool
