package util

import "testing"

func TestGetServiceNameFromEnv(t *testing.T) {
	for _, tc := range []struct {
		name        string
		defaultName string
		envOrder    []string
		env         map[string]string
		want        string
	}{
		{
			name:        "default used when no env vars set",
			defaultName: "fallback",
			envOrder:    []string{"JAEGER_SERVICE_NAME", "OTEL_SERVICE_NAME"},
			env:         map[string]string{},
			want:        "fallback",
		},
		{
			name:        "first env var wins",
			defaultName: "fallback",
			envOrder:    []string{"JAEGER_SERVICE_NAME", "OTEL_SERVICE_NAME"},
			env: map[string]string{
				"JAEGER_SERVICE_NAME": "jaeger-service",
				"OTEL_SERVICE_NAME":   "otel-service",
			},
			want: "jaeger-service",
		},
		{
			name:        "second env var used when first empty",
			defaultName: "fallback",
			envOrder:    []string{"JAEGER_SERVICE_NAME", "OTEL_SERVICE_NAME"},
			env: map[string]string{
				"OTEL_SERVICE_NAME": "otel-service",
			},
			want: "otel-service",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, envVar := range tc.envOrder {
				t.Setenv(envVar, "")
			}
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			if got := GetServiceNameFromEnv(tc.defaultName, tc.envOrder...); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
