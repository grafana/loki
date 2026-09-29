package kafka

import (
	"testing"
	"time"

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
		Topic:                                "abcd",
		ProducerMaxRecordSizeBytes:           1048576,
		ProducerMaxInflightRequestsPerBroker: 20,
		ProducerLinger:                       50 * time.Millisecond,
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

// validProducerConfig returns a Config that passes Validate(), so that each test
// can assert on a single field in isolation.
func validProducerConfig() Config {
	return Config{
		ReaderConfig: ClientConfig{
			Address:  "abcd",
			ClientID: "reader",
		},
		Topic:                                "abcd",
		ProducerMaxRecordSizeBytes:           1048576,
		ProducerMaxInflightRequestsPerBroker: 20,
		ProducerLinger:                       50 * time.Millisecond,
	}
}

func TestProducerMaxInflightRequestsPerBrokerMustBeValid(t *testing.T) {
	tests := []struct {
		name        string
		value       int
		expectedErr error
	}{
		{
			name:  "the default is valid",
			value: 20,
		},
		{
			name:  "one is valid",
			value: 1,
		},
		{
			name:        "zero is invalid",
			value:       0,
			expectedErr: ErrInvalidProducerMaxInflightRequestsPerBroker,
		},
		{
			name:        "negative is invalid",
			value:       -1,
			expectedErr: ErrInvalidProducerMaxInflightRequestsPerBroker,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validProducerConfig()
			cfg.ProducerMaxInflightRequestsPerBroker = tt.value

			err := cfg.Validate()
			if tt.expectedErr != nil {
				require.ErrorIs(t, err, tt.expectedErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestProducerLingerMustBeValid(t *testing.T) {
	tests := []struct {
		name        string
		value       time.Duration
		expectedErr error
	}{
		{
			name:  "the default is valid",
			value: 50 * time.Millisecond,
		},
		{
			name:  "one nanosecond is valid",
			value: time.Nanosecond,
		},
		{
			name:  "zero is valid, and disables lingering",
			value: 0,
		},
		{
			name:        "negative is invalid",
			value:       -time.Millisecond,
			expectedErr: ErrInvalidProducerLinger,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validProducerConfig()
			cfg.ProducerLinger = tt.value

			err := cfg.Validate()
			if tt.expectedErr != nil {
				require.ErrorIs(t, err, tt.expectedErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
