package flagext

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

var flagNameReplacer = strings.NewReplacer(".", "_", "-", "_")

// FlagToEnvVar converts a flag name to its corresponding environment variable name.
// The flag name is uppercased and all dots, dashes, and underscores in the flag name
// are replaced with (or kept as) underscores. The prefix is similarly upper-cased with
// symbol substitution. If prefix is non-empty, an _ is placed between the flag name and prefix
//
// Note that distinct flag names can map to the same environment variable. For example,
// "a.b" and "a-b" both map to "A_B". Callers should avoid registering flags whose
// names differ only in dots, dashes, and underscores.
//
// For example, FlagToEnvVar("myapp-name", "server.http-listen-port") returns
// "MYAPP_NAME_SERVER_HTTP_LISTEN_PORT".
func FlagToEnvVar(prefix, flagName string) string {
	envVar := strings.ToUpper(flagName)
	envVar = flagNameReplacer.Replace(envVar)
	if prefix != "" {
		prefix = strings.ToUpper(prefix)
		prefix = flagNameReplacer.Replace(prefix)
		return prefix + "_" + envVar
	}
	return envVar
}

// SetFlagsFromEnv sets flag values from environment variables for any flags
// that were not explicitly set on the command line. It must be called after
// f.Parse() so that explicitly-set CLI flags can be detected.
//
// The environment variable name for each flag is derived by [FlagToEnvVar].
// CLI flags always take precedence over environment variables, and environment
// variables take precedence over default values.
func SetFlagsFromEnv(f *flag.FlagSet, prefix string) error {
	return SetFlagsFromEnvWithLookup(f, prefix, os.LookupEnv)
}

// SetFlagsFromEnvWithLookup is like [SetFlagsFromEnv] but uses the provided
// lookup function instead of [os.LookupEnv]. This is useful for testing or for
// providing an alternative source of configuration values.
func SetFlagsFromEnvWithLookup(f *flag.FlagSet, prefix string, lookup func(string) (string, bool)) error {
	// Collect the set of flags that were explicitly provided on the command line.
	explicitlySet := make(map[string]struct{})
	f.Visit(func(fl *flag.Flag) {
		explicitlySet[fl.Name] = struct{}{}
	})

	var errs []error
	f.VisitAll(func(fl *flag.Flag) {
		if _, ok := explicitlySet[fl.Name]; ok {
			return // CLI flag takes precedence.
		}

		envVar := FlagToEnvVar(prefix, fl.Name)
		val, ok := lookup(envVar)
		if !ok {
			return // Environment variable not set.
		}

		if err := f.Set(fl.Name, val); err != nil {
			errs = append(errs, fmt.Errorf("setting flag %q from environment variable %s: %w", fl.Name, envVar, err))
		}
	})

	return errors.Join(errs...)
}
