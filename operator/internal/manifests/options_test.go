package manifests

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
)

func TestNewServerConfig_ReturnsDefaults_WhenLimitsSpecEmpty(t *testing.T) {
	s := lokiv1.LokiStack{}

	got, err := NewServerConfig(s.Spec.Limits)
	require.NoError(t, err)
	require.Equal(t, defaultServerConfig(), got)
}

func TestNewServerConfig_ReturnsCustomConfig_WhenLimitsSpecNotEmpty(t *testing.T) {
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

	got, err := NewServerConfig(s.Spec.Limits)
	require.NoError(t, err)

	want := ServerConfig{
		HTTP: &HTTPConfig{
			IdleTimeout:                 30 * time.Second,
			ReadTimeout:                 1 * time.Minute,
			WriteTimeout:                11 * time.Minute,
			GatewayReadTimeout:          1*time.Minute + gatewayReadWiggleRoom,
			GatewayWriteTimeout:         11*time.Minute + gatewayWriteWiggleRoom,
			GatewayUpstreamWriteTimeout: 11 * time.Minute,
		},
	}

	require.Equal(t, want, got)
}

func TestNewServerConfig_ReturnsCustomConfig_WhenLimitsSpecNotEmpty_UseMaxTenantQueryTimeout(t *testing.T) {
	s := lokiv1.LokiStack{
		Spec: lokiv1.LokiStackSpec{
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					QueryLimits: &lokiv1.QueryLimitSpec{
						QueryTimeout: "10m",
					},
				},
				Tenants: map[string]lokiv1.LimitsTemplateSpec{
					"tenant-a": {
						QueryLimits: &lokiv1.QueryLimitSpec{
							QueryTimeout: "10m",
						},
					},
					"tenant-b": {
						QueryLimits: &lokiv1.QueryLimitSpec{
							QueryTimeout: "20m",
						},
					},
				},
			},
		},
	}

	got, err := NewServerConfig(s.Spec.Limits)
	require.NoError(t, err)

	want := ServerConfig{
		HTTP: &HTTPConfig{
			IdleTimeout:                 30 * time.Second,
			ReadTimeout:                 2 * time.Minute,
			WriteTimeout:                21 * time.Minute,
			GatewayReadTimeout:          2*time.Minute + gatewayReadWiggleRoom,
			GatewayWriteTimeout:         21*time.Minute + gatewayWriteWiggleRoom,
			GatewayUpstreamWriteTimeout: 21 * time.Minute,
		},
	}

	require.Equal(t, want, got)
}

func TestNewServerConfig_ReturnsDefaults_WhenGlobalQueryTimeoutParseError(t *testing.T) {
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

	got, err := NewServerConfig(s.Spec.Limits)
	require.Error(t, err)
	require.Equal(t, defaultServerConfig(), got)
}

func TestNewServerConfig_ReturnsDefaults_WhenTenantQueryTimeoutParseError(t *testing.T) {
	s := lokiv1.LokiStack{
		Spec: lokiv1.LokiStackSpec{
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					QueryLimits: &lokiv1.QueryLimitSpec{
						QueryTimeout: "10m",
					},
				},
				Tenants: map[string]lokiv1.LimitsTemplateSpec{
					"tenant-a": {
						QueryLimits: &lokiv1.QueryLimitSpec{
							QueryTimeout: "invalid",
						},
					},
					"tenant-b": {
						QueryLimits: &lokiv1.QueryLimitSpec{
							QueryTimeout: "20m",
						},
					},
				},
			},
		},
	}

	got, err := NewServerConfig(s.Spec.Limits)
	require.Error(t, err)
	require.Equal(t, defaultServerConfig(), got)
}
