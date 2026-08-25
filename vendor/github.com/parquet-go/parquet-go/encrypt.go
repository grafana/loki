package parquet

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Module type constants per the Apache Parquet encryption spec.
const (
	footerModule          byte = 0
	columnMetaDataModule  byte = 1
	dataPageBodyModule    byte = 2
	dataPageHeaderModule  byte = 3
	dictPageBodyModule    byte = 4
	dictPageHeaderModule  byte = 5
	bloomFilterHdrModule  byte = 6
	bloomFilterBitsModule byte = 7
	columnIndexModule     byte = 8
	offsetIndexModule     byte = 9
)

const (
	encNonceSize = 12
	encTagSize   = 16
	// encOverhead is the total per-module overhead: 4-byte length + nonce + GCM tag.
	encOverhead = 4 + encNonceSize + encTagSize
)

// EncryptionAlgorithmType selects the Parquet encryption algorithm.
type EncryptionAlgorithmType int

const (
	// AES_GCM_V1 is the default algorithm (AES-GCM authenticated encryption).
	AES_GCM_V1 EncryptionAlgorithmType = iota
	// AES_GCM_CTR_V1 is the CTR variant (deprecated, included for spec completeness).
	AES_GCM_CTR_V1
)

// EncryptionConfig holds the write-side encryption configuration.
type EncryptionConfig struct {
	// FooterKey is the AES key used to encrypt the footer and any columns
	// that do not have a per-column key. Must be 16, 24, or 32 bytes.
	FooterKey []byte

	// ColumnKeys maps a dot-joined column path (e.g. "a.b.c") to its AES key.
	// Columns not listed here use FooterKey.
	ColumnKeys map[string][]byte

	// Algorithm selects AES_GCM_V1 (default) or AES_GCM_CTR_V1.
	Algorithm EncryptionAlgorithmType

	// EncryptedFooter controls the file layout:
	//   true  → encrypted footer, file uses "PARE" magic.
	//   false → plaintext footer with GCM signature appended, file uses "PAR1" magic.
	EncryptedFooter bool

	// AadPrefix is an optional byte prefix that is prepended to every AAD.
	AadPrefix []byte

	// FileIdentifier is an 8-byte per-file unique value embedded in every AAD.
	// If nil, 8 random bytes are generated when the writer is created.
	FileIdentifier []byte
}

// ErrKeyNotFound is the sentinel error that a KeyRetriever should return (or
// wrap with %w) when the caller intentionally does not have access to a
// particular column key.  OpenFile treats this as a non-fatal signal and
// leaves that column inaccessible rather than aborting the open.  Any other
// error from ColumnKey is treated as a hard failure and propagated to the
// caller.
var ErrKeyNotFound = errors.New("parquet: encryption key not found")

// KeyRetriever resolves AES keys from the metadata bytes stored in the file.
// Implement this interface to supply keys when opening an encrypted parquet file.
type KeyRetriever interface {
	// FooterKey returns the AES key for the file footer.
	// keyMetadata is the optional bytes stored in FileCryptoMetaData.KeyMetadata
	// (may be nil).
	FooterKey(keyMetadata []byte) ([]byte, error)

	// ColumnKey returns the AES key for an encrypted column.
	// path is the column's path in the schema; keyMetadata is the optional bytes
	// stored in EncryptionWithColumnKey.KeyMetadata (may be nil).
	//
	// To signal that a particular column's key is intentionally unavailable
	// (e.g. the caller only holds keys for a subset of columns), return an
	// error that wraps ErrKeyNotFound:
	//
	//	return nil, fmt.Errorf("no key for %v: %w", path, parquet.ErrKeyNotFound)
	//
	// OpenFile treats ErrKeyNotFound as non-fatal and leaves that column
	// inaccessible; any other non-nil error is propagated as a hard failure.
	ColumnKey(path []string, keyMetadata []byte) ([]byte, error)
}

// DecryptionConfig holds the read-side decryption configuration.
type DecryptionConfig struct {
	Keys KeyRetriever
}

// WithDecryption returns a FileOption that configures decryption with the given KeyRetriever.
func WithDecryption(keys KeyRetriever) FileOption {
	return &fileDecryptionOption{cfg: &DecryptionConfig{Keys: keys}}
}

type fileDecryptionOption struct{ cfg *DecryptionConfig }

func (o *fileDecryptionOption) ConfigureFile(c *FileConfig) { c.Decryption = o.cfg }

// WithEncryption returns a WriterOption that configures encryption.
func WithEncryption(cfg *EncryptionConfig) WriterOption {
	return &writerEncryptionOption{cfg: cfg}
}

type writerEncryptionOption struct{ cfg *EncryptionConfig }

func (o *writerEncryptionOption) ConfigureWriter(c *WriterConfig) { c.Encryption = o.cfg }

// -----------------------------------------------------------------------
// Internal state
// -----------------------------------------------------------------------

// fileEncryptionState is the per-file encryption context used during writing.
type fileEncryptionState struct {
	cfg        *EncryptionConfig
	fileUnique []byte // 8-byte random per-file identifier stored in AesGcmV1.AadFileUnique
}

func newFileEncryptionState(cfg *EncryptionConfig) (*fileEncryptionState, error) {
	if cfg.Algorithm == AES_GCM_CTR_V1 {
		return nil, fmt.Errorf("parquet encryption: AES_GCM_CTR_V1 is not yet implemented; use AES_GCM_V1")
	}
	s := &fileEncryptionState{cfg: cfg}
	if len(cfg.FileIdentifier) > 0 {
		s.fileUnique = cfg.FileIdentifier
	} else {
		s.fileUnique = make([]byte, 8)
		if _, err := io.ReadFull(rand.Reader, s.fileUnique); err != nil {
			return nil, fmt.Errorf("parquet encryption: generating file identifier: %w", err)
		}
	}
	return s, nil
}

// columnKeyFor returns the AES key for the given dot-joined column path,
// falling back to FooterKey when no per-column key is configured.
func (s *fileEncryptionState) columnKeyFor(path string) []byte {
	if s.cfg.ColumnKeys != nil {
		if key, ok := s.cfg.ColumnKeys[path]; ok {
			return key
		}
	}
	return s.cfg.FooterKey
}

// -----------------------------------------------------------------------
// Core cryptographic primitives
// -----------------------------------------------------------------------

// encryptModule encrypts plaintext as a Parquet module envelope.
//
// Wire format: 4-byte LE length | 12-byte nonce | AES-GCM(plaintext) | 16-byte tag
// The length field equals len(plaintext) + encNonceSize + encTagSize.
func encryptModule(key, aad, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("parquet encryption: creating AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("parquet encryption: creating GCM: %w", err)
	}

	var nonce [encNonceSize]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, fmt.Errorf("parquet encryption: generating nonce: %w", err)
	}

	// cipherLen = len(plaintext) + gcm.Overhead()  (= tag size = 16)
	cipherLen := len(plaintext) + gcm.Overhead()
	// moduleLen = nonce + ciphertext+tag (= what the 4-byte length field encodes)
	moduleLen := encNonceSize + cipherLen

	out := make([]byte, 4+moduleLen)
	binary.LittleEndian.PutUint32(out[:4], uint32(moduleLen))
	copy(out[4:4+encNonceSize], nonce[:])
	gcm.Seal(out[4+encNonceSize:4+encNonceSize], nonce[:], plaintext, aad)
	return out, nil
}

// decryptModule decrypts a Parquet module envelope.
//
// envelope must start with the 4-byte length field.
// Returns the decrypted plaintext.
func decryptModule(key, aad, envelope []byte) ([]byte, error) {
	if len(envelope) < 4 {
		return nil, fmt.Errorf("parquet decryption: module envelope too short (%d bytes)", len(envelope))
	}
	moduleLen := int(binary.LittleEndian.Uint32(envelope[:4]))
	if len(envelope) < 4+moduleLen {
		return nil, fmt.Errorf("parquet decryption: envelope truncated: need %d bytes, have %d", 4+moduleLen, len(envelope))
	}
	if moduleLen < encNonceSize+encTagSize {
		return nil, fmt.Errorf("parquet decryption: module too short to contain nonce+tag (%d bytes)", moduleLen)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("parquet decryption: creating AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("parquet decryption: creating GCM: %w", err)
	}

	nonce := envelope[4 : 4+encNonceSize]
	ciphertext := envelope[4+encNonceSize : 4+moduleLen]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("parquet decryption: AES-GCM authentication failed: %w", err)
	}
	return plaintext, nil
}

// makeAAD constructs the Additional Authenticated Data for a module.
//
// Layout: aadPrefix || fileUnique || moduleType [ || ordinals... (int16 LE each) ]
func makeAAD(aadPrefix, fileUnique []byte, moduleType byte, ordinals ...int16) []byte {
	buf := make([]byte, 0, len(aadPrefix)+len(fileUnique)+1+len(ordinals)*2)
	buf = append(buf, aadPrefix...)
	buf = append(buf, fileUnique...)
	buf = append(buf, moduleType)
	for _, ord := range ordinals {
		buf = append(buf, byte(ord), byte(ord>>8))
	}
	return buf
}

// signFooter produces the 28-byte footer signature (nonce || GCM tag) used in
// plaintext-footer mode. The footer bytes are authenticated (but not encrypted)
// as additional data with an empty plaintext.
func signFooter(key, aad, footerBytes []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("parquet encryption: footer sign: creating cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("parquet encryption: footer sign: creating GCM: %w", err)
	}

	var nonce [encNonceSize]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, fmt.Errorf("parquet encryption: footer sign: generating nonce: %w", err)
	}

	// Encrypt empty plaintext; footerBytes become the AAD so the tag authenticates them.
	combined := append(aad, footerBytes...)
	tag := gcm.Seal(nil, nonce[:], nil, combined)

	sig := make([]byte, encNonceSize+encTagSize)
	copy(sig[:encNonceSize], nonce[:])
	copy(sig[encNonceSize:], tag)
	return sig, nil
}

// verifyFooterSignature checks the 28-byte signature appended to the footer.
func verifyFooterSignature(key, aad, footerBytes, sig []byte) error {
	if len(sig) != encNonceSize+encTagSize {
		return fmt.Errorf("parquet decryption: footer signature has wrong length %d (want %d)", len(sig), encNonceSize+encTagSize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("parquet decryption: footer verify: creating cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("parquet decryption: footer verify: creating GCM: %w", err)
	}

	nonce := sig[:encNonceSize]
	tag := sig[encNonceSize:]
	combined := append(aad, footerBytes...)
	_, err = gcm.Open(nil, nonce, tag, combined)
	if err != nil {
		return fmt.Errorf("parquet decryption: footer signature verification failed: %w", err)
	}
	return nil
}

// columnPathString returns the dot-joined string for a column path.
func columnPathString(path []string) string {
	return strings.Join(path, ".")
}
