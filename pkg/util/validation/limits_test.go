package validation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// nolint:goconst
func TestSmallestPositiveIntPerTenant(t *testing.T) {
	type args struct {
		tenantIDs []string
		f         func(string) int
	}
	tests := []struct {
		name string
		args args
		want int
	}{
		{
			name: "smallest positive int per tenant",
			args: args{
				tenantIDs: []string{"tenant1", "tenantTwo", "tenantThree"},
				f: func(tenantID string) int {
					// For ease of testing just pick three unique values representing per-tenant limits
					return len(tenantID)
				},
			},
			want: 7, // The smallest of the three
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SmallestPositiveIntPerTenant(tt.args.tenantIDs, tt.args.f); got != tt.want {
				t.Errorf("SmallestPositiveIntPerTenant() = %v, want %v", got, tt.want)
			}
		})
	}
}

// nolint:goconst
func TestSmallestPositiveNonZeroIntPerTenant(t *testing.T) {
	type args struct {
		tenantIDs []string
		f         func(string) int
	}
	tests := []struct {
		name string
		args args
		want int
	}{
		{
			name: "smallest positive non-zero int per tenant",
			args: args{
				tenantIDs: []string{"tenant1", "tenantTwo", "tenantThree"},
				f: func(tenantID string) int {
					// For ease of testing just pick three unique values representing per-tenant limits
					switch tenantID {
					case "tenant1":
						return 1
					case "tenantTwo":
						return 0
					case "tenantThree":
						return 2
					default:
						return len(tenantID)
					}
				},
			},
			want: 1, // The smallest non-zero of the three
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SmallestPositiveNonZeroIntPerTenant(tt.args.tenantIDs, tt.args.f); got != tt.want {
				t.Errorf("SmallestPositiveNonZeroIntPerTenant() = %v, want %v", got, tt.want)
			}
		})
	}
}

// nolint:goconst
func TestSmallestPositiveNonZeroDurationPerTenant(t *testing.T) {
	type args struct {
		tenantIDs []string
		f         func(string) time.Duration
	}
	tests := []struct {
		name string
		args args
		want time.Duration
	}{
		{
			name: "smallest positive non-zero duration per tenant",
			args: args{
				tenantIDs: []string{"tenant1", "tenantTwo", "tenantThree"},
				f: func(tenantID string) time.Duration {
					// For ease of testing just pick three unique values representing per-tenant limits
					switch tenantID {
					case "tenant1":
						return time.Second
					case "tenantTwo":
						return 0
					case "tenantThree":
						return 2 * time.Second
					default:
						return 3 * time.Second
					}
				},
			},
			want: time.Second, // The smallest non-zero of the three
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SmallestPositiveNonZeroDurationPerTenant(tt.args.tenantIDs, tt.args.f); got != tt.want {
				t.Errorf("SmallestPositiveNonZeroDurationPerTenant() = %v, want %v", got, tt.want)
			}
		})
	}
}

// nolint:goconst
func TestMaxDurationPerTenant(t *testing.T) {
	type args struct {
		tenantIDs []string
		f         func(string) time.Duration
	}
	tests := []struct {
		name string
		args args
		want time.Duration
	}{
		{
			name: "max duration per tenant",
			args: args{
				tenantIDs: []string{"tenant1", "tenantTwo", "tenantThree"},
				f: func(tenantID string) time.Duration {
					// For ease of testing just pick three unique values representing per-tenant limits
					switch tenantID {
					case "tenant1":
						return time.Second
					case "tenantTwo":
						return 2 * time.Second
					case "tenantThree":
						return 3 * time.Second
					default:
						return time.Second
					}
				},
			},
			want: 3 * time.Second, // The largest of the three
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaxDurationPerTenant(tt.args.tenantIDs, tt.args.f); got != tt.want {
				t.Errorf("MaxDurationPerTenant() = %v, want %v", got, tt.want)
			}
		})
	}
}

// nolint:goconst
func TestMaxDurationOrZeroPerTenant(t *testing.T) {
	type args struct {
		tenantIDs []string
		f         func(string) time.Duration
	}
	tests := []struct {
		name string
		args args
		want time.Duration
	}{
		{
			name: "max duration or zero per tenant with no zero values",
			args: args{
				tenantIDs: []string{"tenant1", "tenantTwo", "tenantThree"},
				f: func(tenantID string) time.Duration {
					// For ease of testing just pick three unique values representing per-tenant limits
					switch tenantID {
					case "tenant1":
						return time.Second
					case "tenantTwo":
						return 2 * time.Second
					case "tenantThree":
						return 3 * time.Second
					default:
						return time.Second
					}
				},
			},
			want: 3 * time.Second, // The largest of the three with no zero values present
		},
		{
			name: "max duration or zero per tenant with zero values",
			args: args{
				tenantIDs: []string{"tenant1", "tenantTwo", "tenantThree"},
				f: func(tenantID string) time.Duration {
					// For ease of testing just pick three unique values representing per-tenant limits
					switch tenantID {
					case "tenant1":
						return time.Second
					case "tenantTwo":
						return time.Duration(0)
					case "tenantThree":
						return 3 * time.Second
					default:
						return time.Second
					}
				},
			},
			want: time.Duration(0), // Return zero when a single tenant has a zero value
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaxDurationOrZeroPerTenant(tt.args.tenantIDs, tt.args.f); got != tt.want {
				t.Errorf("MaxDurationOrZeroPerTenant() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIntersectionPerTenant(t *testing.T) {
	tests := []struct {
		name      string
		tenantIDs []string
		f         func(string) []string
		expected  []string
	}{
		{
			name:      "no tenants",
			tenantIDs: []string{},
			f: func(_ string) []string {
				return nil
			},
			expected: []string{},
		},
		{
			name:      "single tenant with features",
			tenantIDs: []string{"tenant1"},
			f: func(tenantID string) []string {
				if tenantID == "tenant1" {
					return []string{"featureA", "featureB", "featureC"}
				}
				return nil
			},
			expected: []string{"featureA", "featureB", "featureC"},
		},
		{
			name:      "multiple tenants with common features",
			tenantIDs: []string{"tenant1", "tenant2"},
			f: func(tenantID string) []string {
				if tenantID == "tenant1" {
					return []string{"featureA", "featureB", "featureC"}
				}
				if tenantID == "tenant2" {
					return []string{"featureB", "featureC", "featureD"}
				}
				return nil
			},
			expected: []string{"featureB", "featureC"},
		},
		{
			name:      "multiple tenants with no common features",
			tenantIDs: []string{"tenant1", "tenant2"},
			f: func(tenantID string) []string {
				if tenantID == "tenant1" {
					return []string{"featureA"}
				}
				if tenantID == "tenant2" {
					return []string{"featureB"}
				}
				return nil
			},
			expected: []string{},
		},
		{
			name:      "multiple tenants with overlapping features",
			tenantIDs: []string{"tenant1", "tenant2", "tenant3"},
			f: func(tenantID string) []string {
				if tenantID == "tenant1" {
					return []string{"featureA", "featureB"}
				}
				if tenantID == "tenant2" {
					return []string{"featureB", "featureC"}
				}
				if tenantID == "tenant3" {
					return []string{"featureB", "featureD"}
				}
				return nil
			},
			expected: []string{"featureB"},
		},
		{
			name:      "tenant with empty feature set",
			tenantIDs: []string{"tenant1", "tenant2"},
			f: func(tenantID string) []string {
				if tenantID == "tenant1" {
					return []string{"featureA", "featureB"}
				}
				if tenantID == "tenant2" {
					return []string{}
				}
				return nil
			},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := IntersectionPerTenant(tt.tenantIDs, tt.f)
			require.ElementsMatch(t, actual, tt.expected)
		})
	}
}
