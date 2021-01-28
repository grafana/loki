package aws

import (
	"encoding/base64"
	"encoding/json"
	"flag"

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

// SSEParsedConfig configures server side encryption (SSE)
// struct used internally to configure AWS S3
type SSEParsedConfig struct {
	ServerSideEncryption string
	KMSKeyID             *string
	KMSEncryptionContext *string
}

// SSEConfig configures S3 server side encryption
// struct that is going to receive user input (through config file or CLI)
type SSEConfig struct {
	Type                 string `yaml:"type"`
	KMSKeyID             string `yaml:"kms_key_id"`
	KMSEncryptionContext string `yaml:"kms_encryption_context"`
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *SSEConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.Type, prefix+"type", "", "Enable AWS Server Side Encryption. Only SSE-S3 and SSE-KMS are supported")
	f.StringVar(&cfg.KMSKeyID, prefix+"kms-key-id", "", "KMS Key ID used to encrypt objects in S3")
	f.StringVar(&cfg.KMSEncryptionContext, prefix+"kms-encryption-context", "", "KMS Encryption Context used for object encryption. It expects a JSON as a string.")
}

// NewSSEParsedConfig creates a struct to configure server side encryption (SSE)
func NewSSEParsedConfig(cfg SSEConfig) (*SSEParsedConfig, error) {
	switch cfg.Type {
	case SSES3:
		return &SSEParsedConfig{
			ServerSideEncryption: sseS3Type,
		}, nil
	case SSEKMS:
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

	parsedKMSEncryptionContext := base64.StdEncoding.EncodeToString([]byte(jsonKMSEncryptionContext))

	return &parsedKMSEncryptionContext, nil
}
