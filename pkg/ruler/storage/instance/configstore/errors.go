package configstore

import "fmt"

// ErrNotConnected is used when a store operation was called but no connection
// to the store was active.
var ErrNotConnected = fmt.Errorf("not connected to store")

// NotExistError is used when a config doesn't exist.
type NotExistError struct {
	Key string
}

// Error implements error.
func (e NotExistError) Error() string {
	return fmt.Sprintf("configuration %s does not exist", e.Key)
}

// NotUniqueError is used when two scrape jobs have the same name.
type NotUniqueError struct {
	ScrapeJob string
}

// Error implements error.
func (e NotUniqueError) Error() string {
	return fmt.Sprintf("found multiple scrape configs in config store with job name %q", e.ScrapeJob)
}
