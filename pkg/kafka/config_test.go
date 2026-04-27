package kafka

import (
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
)

func TestBothSASLParamsMustBeSet(t *testing.T) {
	cfg := Config{
		// Other required params
		ReaderConfig: ClientConfig{
			Address:  "abcd",
			ClientID: "reader",
		},
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

func TestSASLMechanismValidation(t *testing.T) {
	t.Parallel()

	baseConfig := func() Config {
		return Config{
			ReaderConfig:               ClientConfig{Address: "addr:9092", ClientID: "reader"},
			Topic:                      "test",
			ProducerMaxRecordSizeBytes: 1048576,
			SASLUsername:               "user",
			SASLPassword:               flagext.SecretWithValue("pass"),
		}
	}

	tests := []struct {
		name      string
		mechanism string
		wantErr   bool
	}{
		{
			name:      "empty string (backward-compatible default)",
			mechanism: "",
			wantErr:   false,
		},
		{
			name:      "PLAIN",
			mechanism: SASLMechanismPlain,
			wantErr:   false,
		},
		{
			name:      "SCRAM-SHA-256",
			mechanism: SASLMechanismScramSHA256,
			wantErr:   false,
		},
		{
			name:      "SCRAM-SHA-512",
			mechanism: SASLMechanismScramSHA512,
			wantErr:   false,
		},
		{
			name:      "unsupported mechanism",
			mechanism: "GSSAPI",
			wantErr:   true,
		},
		{
			name:      "invalid casing",
			mechanism: "plain",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := baseConfig()
			cfg.SASLMechanism = tt.mechanism
			err := cfg.Validate()
			if tt.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, ErrUnsupportedSASLMechanism)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
