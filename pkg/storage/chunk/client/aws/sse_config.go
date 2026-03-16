package aws

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/json"

	"github.com/pkg/errors"

	bucket_s3 "github.com/grafana/loki/v3/pkg/storage/bucket/s3"
)

const (
	sseKMSEncryptionType = "aws:kms"
	sseS3EncryptionType  = "AES256"
	sseCEncryptionType   = "AES256"
)

// SSEParsedConfig configures server side encryption (SSE)
// struct used internally to configure AWS S3
type SSEParsedConfig struct {
	SSEType                     string
	ServerSideEncryption        string
	KMSKeyID                    *string
	KMSEncryptionContext        *string
	CustomerEncryptionKeyB64    *string
	CustomerEncryptionKeyMD5B64 *string
}

// NewSSEParsedConfig creates a struct to configure server side encryption (SSE)
func NewSSEParsedConfig(cfg bucket_s3.SSEConfig) (*SSEParsedConfig, error) {
	switch cfg.Type {
	case bucket_s3.SSES3:
		return &SSEParsedConfig{
			SSEType:              string(bucket_s3.SSES3),
			ServerSideEncryption: sseS3EncryptionType,
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
			SSEType:              string(bucket_s3.SSEKMS),
			ServerSideEncryption: sseKMSEncryptionType,
			KMSKeyID:             &cfg.KMSKeyID,
			KMSEncryptionContext: parsedKMSEncryptionContext,
		}, nil
	case bucket_s3.SSEC:
		if cfg.CustomerEncryptionKey == "" {
			return nil, errors.New("customer encryption key must be passed when SSE-C encryption is selected")
		}
		if len(cfg.CustomerEncryptionKey) != 32 {
			return nil, errors.New("customer encryption key must be 32 bytes long")
		}

		encryptionKeyBytes := []byte(cfg.CustomerEncryptionKey)

		keyHash := md5.Sum(encryptionKeyBytes)

		encryptionKeyBase64 := base64.StdEncoding.EncodeToString(encryptionKeyBytes)
		keyHashBase64 := base64.StdEncoding.EncodeToString(keyHash[:])

		return &SSEParsedConfig{
			SSEType:                     string(bucket_s3.SSEC),
			ServerSideEncryption:        sseCEncryptionType,
			CustomerEncryptionKeyB64:    &encryptionKeyBase64,
			CustomerEncryptionKeyMD5B64: &keyHashBase64,
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
