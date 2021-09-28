package common

// Config holds common config that can be shared between multiple other config sections
type Config struct {
	Store string // This is just an example, but here we should define all the 'common' config values used to set other Loki values.
}
