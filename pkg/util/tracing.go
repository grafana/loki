package util //nolint:revive

import "os"

// GetServiceNameFromEnv returns the first configured service name from the provided
// environment variables, or the supplied default when none are set.
func GetServiceNameFromEnv(defaultName string, envVars ...string) string {
	for _, envVar := range envVars {
		if value := os.Getenv(envVar); value != "" {
			return value
		}
	}

	return defaultName
}
