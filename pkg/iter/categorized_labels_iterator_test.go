package iter

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v4/pkg/logproto"
)

func TestNewCategorizeLabelsIterator(t *testing.T) {
	for _, tc := range []struct {
		name            string
		inner           EntryIterator
		expectedStreams []logproto.Stream
	}{
		{
			name: "no structured metadata nor parsed labels",
			inner: NewSortEntryIterator([]EntryIterator{
				NewStreamIterator(logproto.Stream{
					Labels: labels.FromStrings("namespace", "default").String(),
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(0, 1),
							Line:      "foo=1",
						},
						{
							Timestamp: time.Unix(0, 2),
							Line:      "foo=2",
						},
					},
				}),
			}, logproto.FORWARD),
			expectedStreams: []logproto.Stream{
				{
					Labels: labels.FromStrings("namespace", "default").String(),
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(0, 1),
							Line:      "foo=1",
						},
						{
							Timestamp: time.Unix(0, 2),
							Line:      "foo=2",
						},
					},
				},
			},
		},
		{
			name: "structured metadata and parsed labels",
			inner: NewSortEntryIterator([]EntryIterator{
				NewStreamIterator(logproto.Stream{
					Labels: labels.FromStrings("namespace", "default").String(),
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(0, 1),
							Line:      "foo=1",
						},
					},
				}),
				NewStreamIterator(logproto.Stream{
					Labels: labels.FromStrings("namespace", "default", "traceID", "123").String(),
					Entries: []logproto.Entry{
						{
							Timestamp:          time.Unix(0, 2),
							Line:               "foo=2",
							StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "123")),
						},
					},
				}),
				NewStreamIterator(logproto.Stream{
					Labels: labels.FromStrings("namespace", "default", "foo", "3").String(),
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(0, 3),
							Line:      "foo=3",
							Parsed:    logproto.FromLabelsToLabelAdapters(labels.FromStrings("foo", "3")),
						},
					},
				}),
				NewStreamIterator(logproto.Stream{
					Labels: labels.FromStrings("namespace", "default", "traceID", "123", "foo", "4").String(),
					Entries: []logproto.Entry{
						{
							Timestamp:          time.Unix(0, 4),
							Line:               "foo=4",
							StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "123")),
							Parsed:             logproto.FromLabelsToLabelAdapters(labels.FromStrings("foo", "4")),
						},
					},
				}),
			}, logproto.FORWARD),
			expectedStreams: []logproto.Stream{
				{
					Labels: labels.FromStrings("namespace", "default").String(),
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(0, 1),
							Line:      "foo=1",
						},
						{
							Timestamp:          time.Unix(0, 2),
							Line:               "foo=2",
							StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "123")),
						},
						{
							Timestamp: time.Unix(0, 3),
							Line:      "foo=3",
							Parsed:    logproto.FromLabelsToLabelAdapters(labels.FromStrings("foo", "3")),
						},
						{
							Timestamp:          time.Unix(0, 4),
							Line:               "foo=4",
							StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "123")),
							Parsed:             logproto.FromLabelsToLabelAdapters(labels.FromStrings("foo", "4")),
						},
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			itr := NewCategorizeLabelsIterator(tc.inner)

			streamsEntries := make(map[string][]logproto.Entry)
			for itr.Next() {
				streamsEntries[itr.Labels()] = append(streamsEntries[itr.Labels()], itr.At())
				require.NoError(t, itr.Err())
			}

			var streams []logproto.Stream
			for lbls, entries := range streamsEntries {
				streams = append(streams, logproto.Stream{
					Labels:  lbls,
					Entries: entries,
				})
			}

			require.ElementsMatch(t, tc.expectedStreams, streams)
		})
	}
}

func TestCategorizeLabelsIteratorErr(t *testing.T) {
	t.Run("returns the wrapped iterator error", func(t *testing.T) {
		wantErr := errors.New("retries exceeded")
		itr := NewCategorizeLabelsIterator(&erroringEntryIterator{err: wantErr})

		require.False(t, itr.Next())
		require.ErrorIs(t, itr.Err(), wantErr)
	})

	t.Run("returns a label parse error", func(t *testing.T) {
		itr := NewCategorizeLabelsIterator(NewStreamIterator(logproto.Stream{
			Labels: "not labels",
			Entries: []logproto.Entry{{
				Timestamp:          time.Unix(0, 1),
				Line:               "line",
				StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("traceID", "123")),
			}},
		}))

		require.False(t, itr.Next())
		require.ErrorContains(t, itr.Err(), "failed to parse series labels to categorize labels")
	})
}
