package pipeline

// Configurer defines how clients can retrieve configuration values for use in pipelines.
// For example, a `clone` step might require a remote URL and branch, but how that data is retrieved can change depending on the environment.
// * In Drone, the value is retrieved from an environment variable at runtime. So the only thing this funcion will likely return is the name of the environment variable.
// * In Docker and CLI modes, the remote URL might be provided as a CLI argument or requested via stdin, or even already available with the `git remote` command.
type Configurer interface {
	// Value returns the implementation-specific pipeline config.
	Value(Argument) (string, error)
}
