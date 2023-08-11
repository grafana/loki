// SPDX-License-Identifier: AGPL-3.0-only

package mimir_test

import (
	"flag"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/grafana/mimir/pkg/mimir"
	"github.com/grafana/mimir/pkg/storage/bucket"
)

func TestCommonConfigCanBeExtended(t *testing.T) {
	t.Run("flag inheritance", func(t *testing.T) {
		var cfg customExtendedConfig
		fs := flag.NewFlagSet("test", flag.PanicOnError)
		cfg.RegisterFlags(fs, log.NewNopLogger())

		args := []string{"-common.storage.backend", "s3"}
		require.NoError(t, fs.Parse(args))

		require.NoError(t, mimir.InheritCommonFlagValues(log.NewNopLogger(), fs, cfg.MimirConfig.Common, &cfg.MimirConfig, &cfg))

		// Value should be properly inherited.
		require.Equal(t, "s3", cfg.CustomStorage.Backend)

		// Mimir's inheritance should still work.
		require.Equal(t, "s3", cfg.MimirConfig.BlocksStorage.Bucket.Backend)
	})

	t.Run("yaml inheritance", func(t *testing.T) {
		const commonYAMLConfig = `
common:
  storage:
    backend: s3
`

		var cfg customExtendedConfig
		fs := flag.NewFlagSet("test", flag.PanicOnError)
		cfg.RegisterFlags(fs, log.NewNopLogger())

		err := yaml.Unmarshal([]byte(commonYAMLConfig), &cfg)
		require.NoError(t, err)

		// Value should be properly inherited.
		require.Equal(t, "s3", cfg.CustomStorage.Backend)

		// Mimir's inheritance should still work.
		require.Equal(t, "s3", cfg.MimirConfig.BlocksStorage.Bucket.Backend)
	})
}

type customExtendedConfig struct {
	MimirConfig   mimir.Config  `yaml:",inline"`
	CustomStorage bucket.Config `yaml:"custom_storage"`
}

func (c *customExtendedConfig) RegisterFlags(f *flag.FlagSet, logger log.Logger) {
	c.MimirConfig.RegisterFlags(f, logger)
	c.CustomStorage.RegisterFlagsWithPrefix("custom-storage", f, logger)
}

func (c *customExtendedConfig) CommonConfigInheritance() mimir.CommonConfigInheritance {
	return mimir.CommonConfigInheritance{
		Storage: map[string]*bucket.StorageBackendConfig{
			"custom": &c.CustomStorage.StorageBackendConfig,
		},
	}
}

func (c *customExtendedConfig) UnmarshalYAML(value *yaml.Node) error {
	if err := mimir.UnmarshalCommonYAML(value, &c.MimirConfig, c); err != nil {
		return err
	}

	type plain customExtendedConfig
	return value.DecodeWithOptions((*plain)(c), yaml.DecodeOptions{KnownFields: true})
}

func TestMimirConfigCanBeInlined(t *testing.T) {
	const commonYAMLConfig = `
custom_storage:
  backend: s3
`

	var cfg customExtendedConfig
	fs := flag.NewFlagSet("test", flag.PanicOnError)
	cfg.RegisterFlags(fs, log.NewNopLogger())

	err := yaml.Unmarshal([]byte(commonYAMLConfig), &cfg)
	require.NoError(t, err)

	// Value should be properly set.
	require.Equal(t, "s3", cfg.CustomStorage.Backend)
}
