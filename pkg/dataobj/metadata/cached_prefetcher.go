package metadata

import "time"

// warnInterval is the shortest time between two warnings of the prefetcher, or of its region loader,
// about the objects that they read. It keeps a failing cache from flooding the log.
const warnInterval = 10 * time.Second
