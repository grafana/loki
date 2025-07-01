package chunkenc

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
	"unsafe"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compression"
)

func TestSymbolizer(t *testing.T) {
	for _, tc := range []struct {
		name            string
		labelsToAdd     []labels.Labels
		expectedSymbols []symbols

		expectedNumLabels        int
		expectedCheckpointSize   int
		expectedUncompressedSize int
	}{
		{
			name:                   "no labels",
			expectedCheckpointSize: binary.MaxVarintLen32,
		},
		{
			name: "no duplicate labels",
			labelsToAdd: []labels.Labels{
				labels.FromStrings("foo", "bar"),
				labels.FromStrings(
					"fizz", "buzz",
					"ping", "pong",
				),
			},
			expectedSymbols: []symbols{
				{
					symbol{
						Name:  0,
						Value: 1,
					},
				},
				{
					symbol{
						Name:  2,
						Value: 3,
					},
					symbol{
						Name:  4,
						Value: 5,
					},
				},
			},
			expectedNumLabels:        6,
			expectedCheckpointSize:   binary.MaxVarintLen32 + 6*binary.MaxVarintLen32 + 22,
			expectedUncompressedSize: 22,
		},
		{
			name: "with duplicate labels",
			labelsToAdd: []labels.Labels{
				labels.FromStrings(
					"foo", "bar",
					"bar", "foo",
				),
				labels.FromStrings(
					"foo", "bar",
					"fizz", "buzz",
					"ping", "pong",
				),
			},
			expectedSymbols: []symbols{
				{
					symbol{
						Name:  0,
						Value: 1,
					},
					symbol{
						Name:  1,
						Value: 0,
					},
				},
				{
					symbol{
						Name:  2,
						Value: 3,
					},
					symbol{
						Name:  1,
						Value: 0,
					},
					symbol{
						Name:  4,
						Value: 5,
					},
				},
			},
			expectedNumLabels:        6,
			expectedCheckpointSize:   binary.MaxVarintLen32 + 6*binary.MaxVarintLen32 + 22,
			expectedUncompressedSize: 22,
		},
	} {
		for _, encoding := range testEncodings {
			t.Run(fmt.Sprintf("%s - %s", tc.name, encoding), func(t *testing.T) {
				s := newSymbolizer()
				for i, lbls := range tc.labelsToAdd {
					symbols, err := s.Add(lbls)
					require.NoError(t, err)
					require.Equal(t, tc.expectedSymbols[i], symbols)
					require.Equal(t, lbls, s.Lookup(symbols, nil))
				}

				// Test that Lookup returns empty labels if no symbols are provided.
				if len(tc.labelsToAdd) == 0 {
					ret := s.Lookup([]symbol{
						{
							Name:  0,
							Value: 0,
						},
					}, nil)
					require.Equal(t, `{""=""}`, ret.String())
				}

				require.Equal(t, tc.expectedNumLabels, len(s.labels))
				require.Equal(t, tc.expectedCheckpointSize, s.CheckpointSize())
				require.Equal(t, tc.expectedUncompressedSize, s.UncompressedSize())

				buf := bytes.NewBuffer(nil)
				numBytesWritten, _, err := s.CheckpointTo(buf)
				require.NoError(t, err)
				require.LessOrEqual(t, numBytesWritten, tc.expectedCheckpointSize)

				loaded := symbolizerFromCheckpoint(buf.Bytes())
				for i, symbols := range tc.expectedSymbols {
					require.Equal(t, tc.labelsToAdd[i], loaded.Lookup(symbols, nil))
				}

				buf.Reset()
				_, _, err = s.SerializeTo(buf, compression.GetWriterPool(encoding))
				require.NoError(t, err)

				loaded, err = symbolizerFromEnc(buf.Bytes(), compression.GetReaderPool(encoding))
				require.NoError(t, err)
				for i, symbols := range tc.expectedSymbols {
					require.Equal(t, tc.labelsToAdd[i], loaded.Lookup(symbols, nil))
				}
			})
		}
	}
}

func TestSymbolizerLabelNormalization(t *testing.T) {
	for _, tc := range []struct {
		name           string
		labelsToAdd    []labels.Labels
		expectedLabels []labels.Labels
		description    string
	}{
		{
			name: "basic label normalization",
			labelsToAdd: []labels.Labels{
				labels.FromStrings(
					"foo-bar", "value1",
					"fizz_buzz", "value2",
				),
			},
			expectedLabels: []labels.Labels{
				labels.FromStrings(
					"foo_bar", "value1",
					"fizz_buzz", "value2",
				),
			},
			description: "hyphens should be converted to underscores in label names",
		},
		{
			name: "same string as name and value",
			labelsToAdd: []labels.Labels{
				labels.FromStrings(
					"foo-bar", "foo-bar",
					"fizz-buzz", "fizz-buzz",
				),
			},
			expectedLabels: []labels.Labels{
				labels.FromStrings(
					"foo_bar", "foo-bar",
					"fizz_buzz", "fizz-buzz",
				),
			},
			description: "only normalize when string is used as a name, not as a value",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Test direct addition
			s := newSymbolizer()
			for i, labels := range tc.labelsToAdd {
				symbols, err := s.Add(labels)
				require.NoError(t, err)
				result := s.Lookup(symbols, nil)
				require.Equal(t, tc.expectedLabels[i], result, "direct addition: %s", tc.description)
			}

			// Test serialization/deserialization via checkpoint
			buf := bytes.NewBuffer(nil)
			_, _, err := s.CheckpointTo(buf)
			require.NoError(t, err)

			loaded := symbolizerFromCheckpoint(buf.Bytes())
			for i, labels := range tc.labelsToAdd {
				symbols, err := s.Add(labels)
				require.NoError(t, err)
				result := loaded.Lookup(symbols, nil)
				require.Equal(t, tc.expectedLabels[i], result, "after checkpoint: %s", tc.description)
			}

			// Test serialization/deserialization via compression
			buf.Reset()
			_, _, err = s.SerializeTo(buf, compression.GetWriterPool(compression.Snappy))
			require.NoError(t, err)

			loaded, err = symbolizerFromEnc(buf.Bytes(), compression.GetReaderPool(compression.Snappy))
			require.NoError(t, err)
			for i, labels := range tc.labelsToAdd {
				symbols, err := s.Add(labels)
				require.NoError(t, err)
				result := loaded.Lookup(symbols, nil)
				require.Equal(t, tc.expectedLabels[i], result, "after compression: %s", tc.description)
			}
		})
	}
}

func TestSymbolizerNormalizationCache(t *testing.T) {
	s := newSymbolizer()

	// Add a label with a name that needs normalization
	labels1 := labels.FromStrings("foo-bar", "value1")
	symbols1, err := s.Add(labels1)
	require.NoError(t, err)

	// Look up the label multiple times
	for i := 0; i < 3; i++ {
		result := s.Lookup(symbols1, nil)
		require.Equal(t, "value1", result.Get("foo_bar"), "value should remain unchanged")
	}

	// Add the same label name with a different value
	labels2 := labels.FromStrings("foo-bar", "value2")
	symbols2, err := s.Add(labels2)
	require.NoError(t, err)

	// The normalized name should be reused
	result := s.Lookup(symbols1, nil)
	firstPtr := unsafe.StringData(result[0].Name)
	result = s.Lookup(symbols2, nil)
	secondPtr := unsafe.StringData(result[0].Name)
	require.Equal(t, firstPtr, secondPtr, "normalized name string data pointers should be identical")
	require.Equal(t, "value2", result[0].Value, "new value should be used")

	// Check that we have only one entry in normalizedNames for this label name
	require.Equal(t, 1, len(s.normalizedNames), "should have only one normalized name entry")
}

func TestSymbolizerLabelNormalizationAfterCheckpointing(t *testing.T) {
	s := newSymbolizer()

	// Add some labels and serialize them
	originalLabels := labels.FromStrings(
		"foo-bar", "value1",
		"fizz-buzz", "value2",
	)
	_, err := s.Add(originalLabels)
	require.NoError(t, err)

	buf := bytes.NewBuffer(nil)
	_, _, err = s.CheckpointTo(buf)
	require.NoError(t, err)

	// Load the serializer from checkpoint
	loaded := symbolizerFromCheckpoint(buf.Bytes())

	// Add new labels with the same names but different values
	newLabels := labels.FromStrings(
		"foo-bar", "new-value1",
		"fizz-buzz", "new-value2",
	)
	symbols, err := loaded.Add(newLabels)
	require.NoError(t, err)

	// Check that the normalization is consistent
	result := loaded.Lookup(symbols, nil)
	expected := map[string]string{
		"foo_bar":   "new-value1",
		"fizz_buzz": "new-value2",
	}

	require.Equal(t, expected, result.Map(), "label names should be normalized")
}

func TestSymbolizerLabelNormalizationSameNameValue(t *testing.T) {
	s := newSymbolizer()

	// Add labels where the name and value are the same string
	originalLabels := labels.Labels{
		{Name: "foo-bar", Value: "foo-bar"},
		{Name: "test-label", Value: "test-label"},
	}
	originalSymbols, err := s.Add(originalLabels)
	require.NoError(t, err)

	// Verify initial state
	result := s.Lookup(originalSymbols, nil)
	require.Equal(t, "foo_bar", result[0].Name, "name should be normalized")
	require.Equal(t, "foo-bar", result[0].Value, "value should remain unchanged")
	require.Equal(t, "test_label", result[1].Name, "name should be normalized")
	require.Equal(t, "test-label", result[1].Value, "value should remain unchanged")

	// Serialize the symbolizer
	buf := bytes.NewBuffer(nil)
	_, _, err = s.SerializeTo(buf, compression.GetWriterPool(compression.Snappy))
	require.NoError(t, err)

	// Load the serialized data
	loaded, err := symbolizerFromEnc(buf.Bytes(), compression.GetReaderPool(compression.Snappy))
	require.NoError(t, err)
	require.True(t, loaded.readOnly)

	// trying to add values to symbolizer loaded from serialized data should throw an error
	_, err = loaded.Add(labels.Labels{{Name: "foo-bar2", Value: "foo-bar2"}})
	require.EqualError(t, err, errSymbolizerReadOnly.Error())

	// Look up using the original symbols without re-adding the labels
	result = loaded.Lookup(originalSymbols, nil)
	require.Equal(t, "foo_bar", result[0].Name, "name should be normalized after deserialization")
	require.Equal(t, "foo-bar", result[0].Value, "value should remain unchanged after deserialization")
	require.Equal(t, "test_label", result[1].Name, "name should be normalized after deserialization")
	require.Equal(t, "test-label", result[1].Value, "value should remain unchanged after deserialization")

	// Also test with checkpoint serialization
	buf.Reset()
	_, _, err = s.CheckpointTo(buf)
	require.NoError(t, err)

	loadedFromCheckpoint := symbolizerFromCheckpoint(buf.Bytes())
	result = loadedFromCheckpoint.Lookup(originalSymbols, nil)
	require.Equal(t, "foo_bar", result[0].Name, "name should be normalized after checkpoint")
	require.Equal(t, "foo-bar", result[0].Value, "value should remain unchanged after checkpoint")
	require.Equal(t, "test_label", result[1].Name, "name should be normalized after checkpoint")
	require.Equal(t, "test-label", result[1].Value, "value should remain unchanged after checkpoint")
}
