package rangeio

import "context"

type contextKey struct{}

// WithConfig creates a new context that contains the provided [Config]. Calls
// to [ReadRanges] with this context will use the config.
func WithConfig(ctx context.Context, config *Config) context.Context {
	return context.WithValue(ctx, contextKey{}, config)
}

// configFromContext retrieves the [Config] from the provided context. If the
// context does not contain a config, it returns nil.
func configFromContext(ctx context.Context) *Config {
	config, ok := ctx.Value(contextKey{}).(*Config)
	if !ok {
		return nil
	}
	return config
}
