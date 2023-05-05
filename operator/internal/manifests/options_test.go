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
				Server: &lokiv1.ServerLimitsSpec{
					HTTP: &lokiv1.HttpServerLimitsSpec{
						IdleTimeout:  "1m",
						ReadTimeout:  "10m",
						WriteTimeout: "20m",
					},
				},
			},
		},
	}

	got, err := NewServerConfig(s.Spec.Limits)
	require.NoError(t, err)

	want := ServerConfig{
		HTTP: &HTTPConfig{
			IdleTimeout:                 1 * time.Minute,
			ReadTimeout:                 10 * time.Minute,
			WriteTimeout:                20 * time.Minute,
			GatewayReadTimeout:          10*time.Minute + gatewayReadWiggleRoom,
			GatewayWriteTimeout:         20*time.Minute + gatewayWriteWiggleRoom,
			GatewayUpstreamWriteTimeout: 20 * time.Minute,
		},
	}

	require.Equal(t, want, got)
}

func TestNewServerConfig_ReturnsDefaults_WhenIdleTimeoutParseError(t *testing.T) {
	s := lokiv1.LokiStack{
		Spec: lokiv1.LokiStackSpec{
			Limits: &lokiv1.LimitsSpec{
				Server: &lokiv1.ServerLimitsSpec{
					HTTP: &lokiv1.HttpServerLimitsSpec{
						IdleTimeout:  "invalid",
						ReadTimeout:  "10m",
						WriteTimeout: "20m",
					},
				},
			},
		},
	}

	got, err := NewServerConfig(s.Spec.Limits)
	require.Error(t, err)
	require.Equal(t, defaultServerConfig(), got)
}

func TestNewServerConfig_ReturnsDefaults_WhenReadTimeoutParseError(t *testing.T) {
	s := lokiv1.LokiStack{
		Spec: lokiv1.LokiStackSpec{
			Limits: &lokiv1.LimitsSpec{
				Server: &lokiv1.ServerLimitsSpec{
					HTTP: &lokiv1.HttpServerLimitsSpec{
						IdleTimeout:  "1m",
						ReadTimeout:  "invalid",
						WriteTimeout: "20m",
					},
				},
			},
		},
	}

	got, err := NewServerConfig(s.Spec.Limits)
	require.Error(t, err)
	require.Equal(t, defaultServerConfig(), got)
}

func TestNewServerConfig_ReturnsDefaults_WhenWriteTimeoutParseError(t *testing.T) {
	s := lokiv1.LokiStack{
		Spec: lokiv1.LokiStackSpec{
			Limits: &lokiv1.LimitsSpec{
				Server: &lokiv1.ServerLimitsSpec{
					HTTP: &lokiv1.HttpServerLimitsSpec{
						IdleTimeout:  "1m",
						ReadTimeout:  "10m",
						WriteTimeout: "invalid",
					},
				},
			},
		},
	}

	got, err := NewServerConfig(s.Spec.Limits)
	require.Error(t, err)
	require.Equal(t, defaultServerConfig(), got)
}
