package aws

import (
	"encoding/base64"
	"encoding/json"

	"github.com/pkg/errors"

	bucket_s3 "github.com/grafana/loki/v3/pkg/storage/bucket/s3"
)

const (
	sseKMSType = "aws:kms"
	sseS3Type  = "AES256"
)

// SSEParsedConfig configures server side encryption (SSE)
// struct used internally to configure AWS S3
type SSEParsedConfig struct {
	ServerSideEncryption string
	KMSKeyID             *string
	KMSEncryptionContext *string
}

// NewSSEParsedConfig creates a struct to configure server side encryption (SSE)
func NewSSEParsedConfig(cfg bucket_s3.SSEConfig) (*SSEParsedConfig, error) {
	switch cfg.Type {
	case bucket_s3.SSES3:
		return &SSEParsedConfig{
			ServerSideEncryption: sseS3Type,
		}, nil
	case bucket_s3.SSEKMS:
		if cfg.KMSKeyID == "" {
			return nil, errors.New("KMS key id must be passed when SSE-KMS encryption is selected")
		}

		parsedKMSEncryptionContext, err := parseKMSEncryptionContext(cfg.KMSEncryptionContext)
		if err != nil {
			return nil, errors.Wrap(err, "failed to parse KMS encryption context")
		}

		return &SSEParsedConfig{
			ServerSideEncryption: sseKMSType,
			KMSKeyID:             &cfg.KMSKeyID,
			KMSEncryptionContext: parsedKMSEncryptionContext,
		}, nil
	default:
		return nil, errors.New("SSE type is empty or invalid")
	}
}

func parseKMSEncryptionContext(kmsEncryptionContext string) (*string, error) {
	if kmsEncryptionContext == "" {
		return nil, nil
	}

	// validates if kmsEncryptionContext is a valid JSON
	jsonKMSEncryptionContext, err := json.Marshal(json.RawMessage(kmsEncryptionContext))
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal KMS encryption context")
	}

	parsedKMSEncryptionContext := base64.StdEncoding.EncodeToString(jsonKMSEncryptionContext)

	return &parsedKMSEncryptionContext, nil
}
