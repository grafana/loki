package oci

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	objstoreoci "github.com/thanos-io/objstore/providers/oci"
)

func TestNewObjstoreConfigPreservesHTTPDefaults(t *testing.T) {
	cfg := Config{
		Provider: "instance-principal",
		Bucket:   "loki-data",
	}

	got := newObjstoreConfig(cfg)

	require.Equal(t, objstoreoci.DefaultConfig.HTTPConfig, got.HTTPConfig)
	require.Equal(t, cfg.Provider, got.Provider)
	require.Equal(t, cfg.Bucket, got.Bucket)
}

func TestNewObjstoreConfigOverridesHTTPConfig(t *testing.T) {
	cfg := Config{
		Provider: "instance-principal",
		Bucket:   "loki-data",
		HTTPConfig: objstoreoci.HTTPConfig{
			IdleConnTimeout:    model.Duration(30 * time.Second),
			MaxIdleConns:       25,
			InsecureSkipVerify: true,
		},
	}

	got := newObjstoreConfig(cfg)

	require.Equal(
		t,
		model.Duration(30*time.Second),
		got.HTTPConfig.IdleConnTimeout,
	)
	require.Equal(t, 25, got.HTTPConfig.MaxIdleConns)
	require.True(t, got.HTTPConfig.InsecureSkipVerify)

	// 未显式配置的字段仍采用 Thanos OCI 默认值。
	require.Equal(
		t,
		objstoreoci.DefaultConfig.HTTPConfig.ResponseHeaderTimeout,
		got.HTTPConfig.ResponseHeaderTimeout,
	)
	require.Equal(
		t,
		objstoreoci.DefaultConfig.HTTPConfig.TLSHandshakeTimeout,
		got.HTTPConfig.TLSHandshakeTimeout,
	)
	require.Equal(
		t,
		objstoreoci.DefaultConfig.HTTPConfig.MaxIdleConnsPerHost,
		got.HTTPConfig.MaxIdleConnsPerHost,
	)
	require.Equal(
		t,
		objstoreoci.DefaultConfig.HTTPConfig.ClientTimeout,
		got.HTTPConfig.ClientTimeout,
	)
}
