package metric

import (
	"testing"

	"github.com/pkg/errors"
)

var (
	counterTestTrue  = true
	counterTestFalse = false
	counterTestVal   = "some val"
)

func Test_validateCounterConfig(t *testing.T) {
	tests := []struct {
		name   string
		config CounterConfig
		err    error
	}{
		{"invalid action",
			CounterConfig{
				Action: "del",
			},
			errors.Errorf(ErrCounterInvalidAction, "del"),
		},
		{"invalid counter match all",
			CounterConfig{
				MatchAll: &counterTestTrue,
				Value:    &counterTestVal,
				Action:   "inc",
			},
			errors.New(ErrCounterInvalidMatchAll),
		},
		{"valid",
			CounterConfig{
				Value:  &counterTestVal,
				Action: "inc",
			},
			nil,
		},
		{"valid match all is false",
			CounterConfig{
				MatchAll: &counterTestFalse,
				Value:    &counterTestVal,
				Action:   "inc",
			},
			nil,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateCounterConfig(&tt.config)
			if ((err != nil) && (err.Error() != tt.err.Error())) || (err == nil && tt.err != nil) {
				t.Errorf("Metrics stage validation error, expected error = %v, actual error = %v", tt.err, err)
				return
			}
		})
	}
}
