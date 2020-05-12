package s3

import (
	"flag"

	"github.com/cortexproject/cortex/pkg/util/flagext"
)

// Config holds the config options for an S3 backend
type Config struct {
	Endpoint        string         `yaml:"endpoint"`
	BucketName      string         `yaml:"bucket_name"`
	SecretAccessKey flagext.Secret `yaml:"secret_access_key"`
	AccessKeyID     string         `yaml:"access_key_id"`
	Insecure        bool           `yaml:"insecure"`
}

// RegisterFlags registers the flags for TSDB s3 storage
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.AccessKeyID, "experimental.tsdb.s3.access-key-id", "", "S3 access key ID")
	f.Var(&cfg.SecretAccessKey, "experimental.tsdb.s3.secret-access-key", "S3 secret access key")
	f.StringVar(&cfg.BucketName, "experimental.tsdb.s3.bucket-name", "", "S3 bucket name")
	f.StringVar(&cfg.Endpoint, "experimental.tsdb.s3.endpoint", "", "S3 endpoint without schema")
	f.BoolVar(&cfg.Insecure, "experimental.tsdb.s3.insecure", false, "If enabled, use http:// for the S3 endpoint instead of https://. This could be useful in local dev/test environments while using an S3-compatible backend storage, like Minio.")
}
