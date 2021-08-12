package instance

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

func TestUnmarshalConfig_Valid(t *testing.T) {
	validConfig := DefaultConfig
	validConfigContent, err := yaml.Marshal(validConfig)
	require.NoError(t, err)

	_, err = UnmarshalConfig(bytes.NewReader(validConfigContent))
	require.NoError(t, err)
}

func TestUnmarshalConfig_Invalid(t *testing.T) {
	invalidConfigContent := `whyWouldAnyoneThinkThisisAValidConfig: 12345`

	_, err := UnmarshalConfig(strings.NewReader(invalidConfigContent))
	require.Error(t, err)
}

// TestMarshal_UnmarshalConfig ensures that any method of marshaling an
// instance config does the same thing and retains secrets.
func TestMarshal_UnmarshalConfig(t *testing.T) {
	cfg := `name: test
scrape_configs:
- job_name: local_scrape
  follow_redirects: true
  honor_timestamps: true
  metrics_path: /metrics
  scheme: http
  static_configs:
  - targets:
    - 127.0.0.1:12345
    labels:
      cluster: localhost
  basic_auth:
    username: admin
    password: foobar
remote_write:
- url: http://localhost:9009/api/prom/push
  remote_timeout: 30s
  name: test-d0f32c
  basic_auth:
    username: admin
    password: verysecret
  queue_config:
    capacity: 500
    max_shards: 1000
    min_shards: 1
    max_samples_per_send: 100
    batch_send_deadline: 5s
    min_backoff: 30ms
    max_backoff: 100ms
  follow_redirects: true
  metadata_config:
    send: true
    send_interval: 1m
wal_truncate_frequency: 1m0s
min_wal_time: 5m0s
max_wal_time: 4h0m0s
remote_flush_deadline: 1m0s
`

	t.Run("direct marshal", func(t *testing.T) {
		var c Config
		err := yaml.Unmarshal([]byte(cfg), &c)
		require.NoError(t, err)

		out, err := yaml.Marshal(c)
		require.NoError(t, err)
		require.YAMLEq(t, cfg, string(out))
	})

	t.Run("direct mashal pointer", func(t *testing.T) {
		c := &Config{}
		err := yaml.Unmarshal([]byte(cfg), c)
		require.NoError(t, err)

		out, err := yaml.Marshal(c)
		require.NoError(t, err)
		require.YAMLEq(t, cfg, string(out))
	})

	t.Run("custom marshal methods", func(t *testing.T) {
		c, err := UnmarshalConfig(strings.NewReader(cfg))
		require.NoError(t, err)

		out, err := MarshalConfig(c, false)
		require.NoError(t, err)
		require.YAMLEq(t, cfg, string(out))
	})
}

// TestMarshal_UnmarshalConfig ensures that any method of marshaling an
// instance config does the same thing and retains secrets.
func TestMarshal_UnmarshalConfig_Sigv4(t *testing.T) {
	cfg := `name: test
scrape_configs:
- job_name: local_scrape
  follow_redirects: true
  honor_timestamps: true
  metrics_path: /metrics
  scheme: http
  static_configs:
  - targets:
    - 127.0.0.1:12345
    labels:
      cluster: localhost
  basic_auth:
    username: admin
    password: foobar
remote_write:
- url: http://localhost:9009/api/prom/push
  remote_timeout: 30s
  name: test-d0f32c
  sigv4: {}
  queue_config:
    capacity: 500
    max_shards: 1000
    min_shards: 1
    max_samples_per_send: 100
    batch_send_deadline: 5s
    min_backoff: 30ms
    max_backoff: 100ms
  follow_redirects: true
  metadata_config:
    send: true
    send_interval: 1m
wal_truncate_frequency: 1m0s
min_wal_time: 5m0s
max_wal_time: 4h0m0s
remote_flush_deadline: 1m0s
`

	t.Run("direct marshal", func(t *testing.T) {
		var c Config
		err := yaml.Unmarshal([]byte(cfg), &c)
		require.NoError(t, err)

		out, err := yaml.Marshal(c)
		require.NoError(t, err)
		require.YAMLEq(t, cfg, string(out))
	})

	t.Run("direct mashal pointer", func(t *testing.T) {
		c := &Config{}
		err := yaml.Unmarshal([]byte(cfg), c)
		require.NoError(t, err)

		out, err := yaml.Marshal(c)
		require.NoError(t, err)
		require.YAMLEq(t, cfg, string(out))
	})

	t.Run("custom marshal methods", func(t *testing.T) {
		c, err := UnmarshalConfig(strings.NewReader(cfg))
		require.NoError(t, err)

		out, err := MarshalConfig(c, false)
		require.NoError(t, err)
		require.YAMLEq(t, cfg, string(out))
	})
}
