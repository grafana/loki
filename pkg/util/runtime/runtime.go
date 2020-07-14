package runtime

// TenantOptions is a function that returns options for given tenant, or
// nil, if there are no tenant-specific options.
type TenantOptions func(userID string) *Options

// Options describe all the runtime options that can be configured
type Options struct {
	LogLimits bool `yaml:"log_limits"`
}

type RuntimeOptions struct {
	defaultOptions *Options
	tenantOptions  TenantOptions
}

// NewRuntimeOptions makes a new RuntimeOptions.
func NewRuntimeOptions(tenantOptions TenantOptions) (*RuntimeOptions, error) {
	return &RuntimeOptions{
		// Different from limits, Options do not have defaults outside of code defaults
		// i.e. their defaults are defined here. The only possibility with RuntimeOptions
		// are to change the value away from the default via the runtime overrides mechanism
		defaultOptions: &Options{
			LogLimits: false,
		},
		tenantOptions: tenantOptions,
	}, nil
}

// LogLimits returns if log messages should be displayed for the provided tenant when hitting limits
func (o *RuntimeOptions) LogLimits(tenantID string) bool {
	return o.getOverridesForUser(tenantID).LogLimits
}

func (o *RuntimeOptions) getOverridesForUser(tenantID string) *Options {
	if o.tenantOptions != nil {
		l := o.tenantOptions(tenantID)
		if l != nil {
			return l
		}
	}
	return o.defaultOptions
}
