package manifests

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/internal/config"
)

func TestNewTimeoutConfig_ReturnsDefaults_WhenLimitsSpecEmpty(t *testing.T) {
	s := lokiv1.LokiStack{}

	got, err := NewTimeoutConfig(s.Spec.Limits)
	require.NoError(t, err)
	require.Equal(t, defaultTimeoutConfig, got)
}

func TestNewTimeoutConfig_ReturnsCustomConfig_WhenLimitsSpecNotEmpty(t *testing.T) {
	s := lokiv1.LokiStack{
		Spec: lokiv1.LokiStackSpec{
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					QueryLimits: &lokiv1.QueryLimitSpec{
						QueryTimeout: "10m",
					},
				},
			},
		},
	}

	got, err := NewTimeoutConfig(s.Spec.Limits)
	require.NoError(t, err)

	want := TimeoutConfig{
		Loki: config.HTTPTimeoutConfig{
			IdleTimeout:  30 * time.Second,
			ReadTimeout:  1 * time.Minute,
			WriteTimeout: 11 * time.Minute,
		},
		Gateway: GatewayTimeoutConfig{
			ReadTimeout:          1*time.Minute + gatewayReadDuration,
			WriteTimeout:         11*time.Minute + gatewayWriteDuration,
			UpstreamWriteTimeout: 11 * time.Minute,
		},
	}

	require.Equal(t, want, got)
}

func TestNewTimeoutConfig_ReturnsCustomConfig_WhenLimitsSpecNotEmpty_UseMaxTenantQueryTimeout(t *testing.T) {
	s := lokiv1.LokiStack{
		Spec: lokiv1.LokiStackSpec{
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					QueryLimits: &lokiv1.QueryLimitSpec{
						QueryTimeout: "10m",
					},
				},
				Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
					"tenant-a": {
						QueryLimits: &lokiv1.PerTenantQueryLimitSpec{
							QueryLimitSpec: lokiv1.QueryLimitSpec{
								QueryTimeout: "10m",
							},
						},
					},
					"tenant-b": {
						QueryLimits: &lokiv1.PerTenantQueryLimitSpec{
							QueryLimitSpec: lokiv1.QueryLimitSpec{
								QueryTimeout: "20m",
							},
						},
					},
				},
			},
		},
	}

	got, err := NewTimeoutConfig(s.Spec.Limits)
	require.NoError(t, err)

	want := TimeoutConfig{
		Loki: config.HTTPTimeoutConfig{
			IdleTimeout:  30 * time.Second,
			ReadTimeout:  2 * time.Minute,
			WriteTimeout: 21 * time.Minute,
		},
		Gateway: GatewayTimeoutConfig{
			ReadTimeout:          2*time.Minute + gatewayReadDuration,
			WriteTimeout:         21*time.Minute + gatewayWriteDuration,
			UpstreamWriteTimeout: 21 * time.Minute,
		},
	}

	require.Equal(t, want, got)
}

func TestNewTimeoutConfig_ReturnsCustomConfig_WhenTenantLimitsSpecOnly_ReturnsUseMaxTenantQueryTimeout(t *testing.T) {
	s := lokiv1.LokiStack{
		Spec: lokiv1.LokiStackSpec{
			Limits: &lokiv1.LimitsSpec{
				Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
					"tenant-a": {
						QueryLimits: &lokiv1.PerTenantQueryLimitSpec{
							QueryLimitSpec: lokiv1.QueryLimitSpec{
								QueryTimeout: "10m",
							},
						},
					},
					"tenant-b": {
						QueryLimits: &lokiv1.PerTenantQueryLimitSpec{
							QueryLimitSpec: lokiv1.QueryLimitSpec{
								QueryTimeout: "20m",
							},
						},
					},
				},
			},
		},
	}

	got, err := NewTimeoutConfig(s.Spec.Limits)
	require.NoError(t, err)

	want := TimeoutConfig{
		Loki: config.HTTPTimeoutConfig{
			IdleTimeout:  30 * time.Second,
			ReadTimeout:  2 * time.Minute,
			WriteTimeout: 21 * time.Minute,
		},
		Gateway: GatewayTimeoutConfig{
			ReadTimeout:          2*time.Minute + gatewayReadDuration,
			WriteTimeout:         21*time.Minute + gatewayWriteDuration,
			UpstreamWriteTimeout: 21 * time.Minute,
		},
	}

	require.Equal(t, want, got)
}

func TestNewTimeoutConfig_ReturnsDefaults_WhenGlobalQueryTimeoutParseError(t *testing.T) {
	s := lokiv1.LokiStack{
		Spec: lokiv1.LokiStackSpec{
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					QueryLimits: &lokiv1.QueryLimitSpec{
						QueryTimeout: "invalid",
					},
				},
			},
		},
	}

	_, err := NewTimeoutConfig(s.Spec.Limits)
	require.Error(t, err)
}

func TestNewTimeoutConfig_ReturnsDefaults_WhenTenantQueryTimeoutParseError(t *testing.T) {
	s := lokiv1.LokiStack{
		Spec: lokiv1.LokiStackSpec{
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					QueryLimits: &lokiv1.QueryLimitSpec{
						QueryTimeout: "10m",
					},
				},
				Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
					"tenant-a": {
						QueryLimits: &lokiv1.PerTenantQueryLimitSpec{
							QueryLimitSpec: lokiv1.QueryLimitSpec{
								QueryTimeout: "invalid",
							},
						},
					},
					"tenant-b": {
						QueryLimits: &lokiv1.PerTenantQueryLimitSpec{
							QueryLimitSpec: lokiv1.QueryLimitSpec{
								QueryTimeout: "20m",
							},
						},
					},
				},
			},
		},
	}

	_, err := NewTimeoutConfig(s.Spec.Limits)
	require.Error(t, err)
}
