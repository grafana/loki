package kafka

import (
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
)

func TestBothSASLParamsMustBeSet(t *testing.T) {
	cfg := Config{
		// Other required params
		Address:                    "abcd",
		Topic:                      "abcd",
		ProducerMaxRecordSizeBytes: 1048576,
	}

	// No SASL params is valid
	err := cfg.Validate()
	require.NoError(t, err)

	// Just username is invalid
	cfg.SASLUsername = "abcd"
	cfg.SASLPassword = flagext.Secret{}
	err = cfg.Validate()
	require.Error(t, err)

	// Just password is invalid
	cfg.SASLUsername = ""
	cfg.SASLPassword = flagext.SecretWithValue("abcd")
	err = cfg.Validate()
	require.Error(t, err)

	// Both username and password is valid
	cfg.SASLUsername = "abcd"
	cfg.SASLPassword = flagext.SecretWithValue("abcd")
	err = cfg.Validate()
	require.NoError(t, err)
}

func TestAmbiguousKafkaAddress(t *testing.T) {
	cfg := Config{
		Address:      "localhost:9092",
		ReaderConfig: ClientConfig{Address: "localhost:9092"},
		WriterConfig: ClientConfig{Address: "localhost:9092"},
	}
	err := cfg.Validate()
	require.Error(t, err)
	require.ErrorIs(t, err, ErrAmbiguousKafkaAddress)
}

func TestAmbiguousKafkaClientID(t *testing.T) {
	cfg := Config{
		ClientID:     "abcd",
		ReaderConfig: ClientConfig{Address: "reader:9092", ClientID: "abcd"},
		WriterConfig: ClientConfig{Address: "writer:9092", ClientID: "abcd"},
	}
	err := cfg.Validate()
	require.Error(t, err)
	require.ErrorIs(t, err, ErrAmbiguousKafkaClientID)
}
