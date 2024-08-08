package chunkenc

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
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
				{
					labels.Label{
						Name:  "foo",
						Value: "bar",
					},
				},
				{
					labels.Label{
						Name:  "fizz",
						Value: "buzz",
					},
					labels.Label{
						Name:  "ping",
						Value: "pong",
					},
				},
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
				{
					labels.Label{
						Name:  "foo",
						Value: "bar",
					},
					{
						Name:  "bar",
						Value: "foo",
					},
				},
				{
					labels.Label{
						Name:  "foo",
						Value: "bar",
					},
					labels.Label{
						Name:  "fizz",
						Value: "buzz",
					},
					labels.Label{
						Name:  "ping",
						Value: "pong",
					},
				},
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
						Name:  0,
						Value: 1,
					},
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
	} {
		for _, encoding := range testEncoding {
			t.Run(fmt.Sprintf("%s - %s", tc.name, encoding), func(t *testing.T) {
				s := newSymbolizer()
				for i, labels := range tc.labelsToAdd {
					symbols := s.Add(labels)
					require.Equal(t, tc.expectedSymbols[i], symbols)
					require.Equal(t, labels, s.Lookup(symbols, nil))
				}

				// Test that Lookup returns empty labels if no symbols are provided.
				if len(tc.labelsToAdd) == 0 {
					ret := s.Lookup([]symbol{
						{
							Name:  0,
							Value: 0,
						},
					}, nil)
					require.Equal(t, "", ret[0].Name)
					require.Equal(t, "", ret[0].Value)
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
				_, _, err = s.SerializeTo(buf, GetWriterPool(encoding))
				require.NoError(t, err)

				loaded, err = symbolizerFromEnc(buf.Bytes(), GetReaderPool(encoding))
				require.NoError(t, err)
				for i, symbols := range tc.expectedSymbols {
					require.Equal(t, tc.labelsToAdd[i], loaded.Lookup(symbols, nil))
				}
			})
		}
	}
}
