package server

// cmdMeta holds metadata about a registered command
type cmdMeta struct {
	handler  Cmd
	readOnly bool
}

// CmdOption is a function that configures command metadata
type CmdOption func(*cmdMeta)

// ReadOnlyOption marks a command as read-only
func ReadOnlyOption() CmdOption {
	return func(meta *cmdMeta) {
		meta.readOnly = true
	}
}
