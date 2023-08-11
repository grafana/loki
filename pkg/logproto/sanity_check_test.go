// SPDX-License-Identifier: AGPL-3.0-only

package mimir

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"

	"github.com/grafana/mimir/pkg/storage/bucket"
)

func TestCheckObjectStoresConfig(t *testing.T) {
	tests := map[string]struct {
		setup    func(cfg *Config)
		expected string
	}{
		"should succeed with the default config": {
			setup:    nil,
			expected: "",
		},
		"should succeed with the default config running Alertmanager along with target=all": {
			setup: func(cfg *Config) {
				require.NoError(t, cfg.Target.Set("all,alertmanager"))
			},
			expected: "",
		},
		"should succeed with filesystem backend and non-existent directory (components create the dir at startup)": {
			setup: func(cfg *Config) {
				require.NoError(t, cfg.Target.Set("all,alertmanager"))

				for _, bucketCfg := range []*bucket.Config{&cfg.BlocksStorage.Bucket, &cfg.AlertmanagerStorage.Config, &cfg.RulerStorage.Config} {
					bucketCfg.Backend = bucket.Filesystem
					bucketCfg.Filesystem.Directory = "/does/not/exists"
				}

				cfg.BlocksStorage.Bucket.StoragePrefix = "blocks"
			},
			expected: "",
		},
		"should check only blocks storage config when target=ingester": {
			setup: func(cfg *Config) {
				require.NoError(t, cfg.Target.Set("ingester"))

				// Configure alertmanager and ruler storage to fail, but expect to succeed.
				for _, bucketCfg := range []*bucket.Config{&cfg.AlertmanagerStorage.Config, &cfg.RulerStorage.Config} {
					bucketCfg.Backend = bucket.GCS
					bucketCfg.GCS.BucketName = "invalid"
				}
			},
			expected: "",
		},
		"should check only alertmanager storage config when target=alertmanager": {
			setup: func(cfg *Config) {
				require.NoError(t, cfg.Target.Set("alertmanager"))

				// Configure blocks storage and ruler storage to fail, but expect to succeed.
				for _, bucketCfg := range []*bucket.Config{&cfg.BlocksStorage.Bucket, &cfg.RulerStorage.Config} {
					bucketCfg.Backend = bucket.GCS
					bucketCfg.GCS.BucketName = "invalid"
				}
			},
			expected: "",
		},
		"should check blocks and ruler storage config when target=ruler": {
			setup: func(cfg *Config) {
				require.NoError(t, cfg.Target.Set("ruler"))

				// Configure alertmanager storage to fail, but expect to succeed.
				cfg.AlertmanagerStorage.Config.Backend = bucket.GCS
				cfg.AlertmanagerStorage.Config.GCS.BucketName = "invalid"
			},
			expected: "",
		},
		"should fail on invalid AWS S3 config": {
			setup: func(cfg *Config) {
				require.NoError(t, cfg.Target.Set("all,alertmanager"))

				for i, bucketCfg := range []*bucket.Config{&cfg.BlocksStorage.Bucket, &cfg.AlertmanagerStorage.Config, &cfg.RulerStorage.Config} {
					bucketCfg.Backend = bucket.S3
					bucketCfg.S3.Region = "us-east-1"
					bucketCfg.S3.Endpoint = "s3.dualstack.us-east-1.amazonaws.com"
					bucketCfg.S3.BucketName = "invalid"
					bucketCfg.S3.AccessKeyID = "xxx"
					bucketCfg.S3.SecretAccessKey = flagext.SecretWithValue("yyy")

					// Set a different bucket name for blocks storage to avoid config validation error.
					if i == 0 {
						bucketCfg.S3.BucketName = "invalid"
					} else {
						bucketCfg.S3.BucketName = "invalid-1"
					}
				}
			},
			expected: errObjectStorage,
		},
		"should fail on invalid GCS config": {
			setup: func(cfg *Config) {
				require.NoError(t, cfg.Target.Set("all,alertmanager"))

				for i, bucketCfg := range []*bucket.Config{&cfg.BlocksStorage.Bucket, &cfg.AlertmanagerStorage.Config, &cfg.RulerStorage.Config} {
					bucketCfg.Backend = bucket.GCS

					// Set a different bucket name for blocks storage to avoid config validation error.
					if i == 0 {
						bucketCfg.GCS.BucketName = "invalid"
					} else {
						bucketCfg.GCS.BucketName = "invalid-1"
					}
				}
			},
			expected: errObjectStorage,
		},
		"should fail on invalid Azure config": {
			setup: func(cfg *Config) {
				require.NoError(t, cfg.Target.Set("all,alertmanager"))

				for i, bucketCfg := range []*bucket.Config{&cfg.BlocksStorage.Bucket, &cfg.AlertmanagerStorage.Config, &cfg.RulerStorage.Config} {
					bucketCfg.Backend = bucket.Azure
					bucketCfg.Azure.ContainerName = "invalid"
					bucketCfg.Azure.StorageAccountName = ""
					bucketCfg.Azure.StorageAccountKey = flagext.SecretWithValue("eHh4")

					// Set a different container name for blocks storage to avoid config validation error.
					if i == 0 {
						bucketCfg.Azure.ContainerName = "invalid"
					} else {
						bucketCfg.Azure.ContainerName = "invalid-1"
					}
				}
			},
			expected: errObjectStorage,
		},
		"should fail on invalid Swift config": {
			setup: func(cfg *Config) {
				require.NoError(t, cfg.Target.Set("all,alertmanager"))

				for i, bucketCfg := range []*bucket.Config{&cfg.BlocksStorage.Bucket, &cfg.AlertmanagerStorage.Config, &cfg.RulerStorage.Config} {
					bucketCfg.Backend = bucket.Swift
					bucketCfg.Swift.AuthURL = "http://127.0.0.1/"

					// Set a different project name for blocks storage to avoid config validation error.
					if i == 0 {
						bucketCfg.Swift.ProjectName = "invalid"
					} else {
						bucketCfg.Swift.ProjectName = "invalid-1"
					}
				}
			},
			expected: errObjectStorage,
		},
	}

	for testName, testData := range tests {
		// Change scope since we're running each test in parallel.
		testData := testData

		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			cfg := Config{}
			flagext.DefaultValues(&cfg)

			if testData.setup != nil {
				testData.setup(&cfg)
			}

			require.NoError(t, cfg.Validate(log.NewNopLogger()), "pre-condition: the config static validation should pass")

			actual := checkObjectStoresConfig(context.Background(), cfg, log.NewNopLogger())
			if testData.expected == "" {
				require.NoError(t, actual)
			} else {
				require.Error(t, actual)
				require.Contains(t, actual.Error(), testData.expected)
			}
		})
	}

}

func TestCheckDirectoryReadWriteAccess(t *testing.T) {
	const configuredPath = "/path/to/dir"

	configs := map[string]func(t *testing.T, cfg *Config){
		"ingester": func(t *testing.T, cfg *Config) {
			require.NoError(t, cfg.Target.Set("ingester"))
			cfg.Ingester.BlocksStorageConfig.TSDB.Dir = configuredPath
		},
		"store-gateway": func(t *testing.T, cfg *Config) {
			require.NoError(t, cfg.Target.Set("store-gateway"))
			cfg.BlocksStorage.BucketStore.SyncDir = configuredPath
		},
		"compactor": func(t *testing.T, cfg *Config) {
			require.NoError(t, cfg.Target.Set("compactor"))
			cfg.Compactor.DataDir = configuredPath
		},
		"ruler": func(t *testing.T, cfg *Config) {
			require.NoError(t, cfg.Target.Set("ruler"))
			cfg.Ruler.RulePath = configuredPath
		},
		"alertmanager": func(t *testing.T, cfg *Config) {
			require.NoError(t, cfg.Target.Set("alertmanager"))
			cfg.Alertmanager.DataDir = configuredPath
		},
	}

	tests := map[string]struct {
		dirExistsFn       dirExistsFunc
		isDirReadWritable isDirReadWritableFunc
		expected          string
	}{
		"should fail on directory without write access": {
			dirExistsFn: func(dir string) (bool, error) {
				return true, nil
			},
			isDirReadWritable: func(dir string) error {
				return errors.New("read only")
			},
			expected: fmt.Sprintf("failed to access directory %s: read only", configuredPath),
		},
		"should pass on directory with read-write access": {
			dirExistsFn: func(dir string) (bool, error) {
				return true, nil
			},
			isDirReadWritable: func(dir string) error {
				return nil
			},
			expected: "",
		},
		"should pass if directory doesn't exist but parent existing folder has read-write access": {
			dirExistsFn: func(dir string) (bool, error) {
				return dir == "/", nil
			},
			isDirReadWritable: func(dir string) error {
				if dir == "/" {
					return nil
				}
				return errors.New("not exists")
			},
			expected: "",
		},
		"should fail if directory doesn't exist and parent existing folder has no read-write access": {
			dirExistsFn: func(dir string) (bool, error) {
				return dir == "/", nil
			},
			isDirReadWritable: func(dir string) error {
				if dir == "/" {
					return errors.New("read only")
				}
				return errors.New("not exists")
			},
			expected: fmt.Sprintf("failed to access directory %s: read only", configuredPath),
		},
	}

	for configName, configSetup := range configs {
		t.Run(configName, func(t *testing.T) {
			for testName, testData := range tests {
				t.Run(testName, func(t *testing.T) {
					cfg := Config{}
					flagext.DefaultValues(&cfg)
					configSetup(t, &cfg)

					actual := checkDirectoriesReadWriteAccess(cfg, testData.dirExistsFn, testData.isDirReadWritable)
					if testData.expected == "" {
						require.NoError(t, actual)
					} else {
						require.Error(t, actual)
						require.Contains(t, actual.Error(), testData.expected)
					}
				})
			}
		})
	}
}
