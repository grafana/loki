package aws

import (
	"encoding/base64"
	"encoding/json"

	"github.com/pkg/errors"
)

const (
	// SSEKMS config type constant to configure S3 server side encryption using KMS
	// https://docs.aws.amazon.com/AmazonS3/latest/dev/UsingKMSEncryption.html
	SSEKMS     = "SSE-KMS"
	sseKMSType = "aws:kms"
	// SSES3 config type constant to configure S3 server side encryption with AES-256
	// https://docs.aws.amazon.com/AmazonS3/latest/dev/UsingServerSideEncryption.html
	SSES3     = "SSE-S3"
	sseS3Type = "AES256"
)

// SSEEncryptionConfig configures server side encryption (SSE)
type SSEEncryptionConfig struct {
	ServerSideEncryption string
	KMSKeyID             *string
	KMSEncryptionContext *string
}

// NewSSEEncryptionConfig creates a struct to configure server side encryption (SSE)
func NewSSEEncryptionConfig(sseType string, kmsKeyID *string, kmsEncryptionContext map[string]string) (*SSEEncryptionConfig, error) {
	switch sseType {
	case SSES3:
		return &SSEEncryptionConfig{
			ServerSideEncryption: sseS3Type,
		}, nil
	case SSEKMS:
		if kmsKeyID == nil {
			return nil, errors.New("KMS key id must be passed when SSE-KMS encryption is selected")
		}

		parsedKMSEncryptionContext, err := parseKMSEncryptionContext(kmsEncryptionContext)
		if err != nil {
			return nil, errors.Wrap(err, "failed to parse KMS encryption context")
		}

		return &SSEEncryptionConfig{
			ServerSideEncryption: sseKMSType,
			KMSKeyID:             kmsKeyID,
			KMSEncryptionContext: parsedKMSEncryptionContext,
		}, nil
	default:
		return nil, errors.New("SSE type is empty or invalid")
	}
}

func parseKMSEncryptionContext(kmsEncryptionContext map[string]string) (*string, error) {
	if kmsEncryptionContext == nil {
		return nil, nil
	}

	jsonKMSEncryptionContext, err := json.Marshal(kmsEncryptionContext)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal KMS encryption context")
	}

	parsedKMSEncryptionContext := base64.StdEncoding.EncodeToString([]byte(jsonKMSEncryptionContext))

	return &parsedKMSEncryptionContext, nil
}
