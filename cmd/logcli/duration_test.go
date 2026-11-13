package main

import (
	"testing"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDurationValue_Set(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
		err      bool
	}{
		{input: "0", expected: 0},
		{input: "500ms", expected: 500 * time.Millisecond},
		{input: "30s", expected: 30 * time.Second},
		{input: "5m", expected: 5 * time.Minute},
		{input: "1h", expected: time.Hour},
		{input: "24h", expected: 24 * time.Hour},
		{input: "1d", expected: 24 * time.Hour},
		{input: "7d", expected: 7 * 24 * time.Hour},
		{input: "1w", expected: 7 * 24 * time.Hour},
		{input: "2w", expected: 14 * 24 * time.Hour},
		{input: "1y", expected: 365 * 24 * time.Hour},
		{input: "1w2d3h", expected: (7*24 + 2*24 + 3) * time.Hour},
		{input: "invalid", err: true},
		{input: "-1h", err: true},
		{input: "", err: true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			var d time.Duration
			val := newDurationValue(&d)
			err := val.Set(tc.input)
			if tc.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expected, d)
				assert.Equal(t, tc.expected, val.Get())
			}
		})
	}
}

func TestDurationFlagIntegration(t *testing.T) {
	app := kingpin.New("test", "test app")
	var since time.Duration
	durationVar(app.Flag("since", sinceHelp).Default("1h"), &since)

	// Test default value
	_, err := app.Parse([]string{})
	require.NoError(t, err)
	assert.Equal(t, time.Hour, since)

	// Test days unit (e.g. 1d, 7d)
	_, err = app.Parse([]string{"--since=1d"})
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, since)

	_, err = app.Parse([]string{"--since=7d"})
	require.NoError(t, err)
	assert.Equal(t, 7*24*time.Hour, since)

	// Test weeks unit
	_, err = app.Parse([]string{"--since=2w"})
	require.NoError(t, err)
	assert.Equal(t, 14*24*time.Hour, since)

	// Test years unit
	_, err = app.Parse([]string{"--since=1y"})
	require.NoError(t, err)
	assert.Equal(t, 365*24*time.Hour, since)

	// Test invalid unit
	_, err = app.Parse([]string{"--since=10x"})
	require.Error(t, err)
}
