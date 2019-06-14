package querier

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/metadata"
)

type mockQuerierTailClient struct {
	streams []logproto.Stream
	index   int
}

func (mockQuerierTailClient) Header() (metadata.MD, error) {
	return nil, nil
}

func (mockQuerierTailClient) Trailer() metadata.MD {
	return nil
}

func (mockQuerierTailClient) CloseSend() error {
	return nil
}

func (mockQuerierTailClient) Context() context.Context {
	return nil
}

func (mockQuerierTailClient) SendMsg(i interface{}) error {
	return nil
}

func (mockQuerierTailClient) RecvMsg(i interface{}) error {
	return nil
}

func (m *mockQuerierTailClient) Recv() (*logproto.TailResponse, error) {
	if m.index < len(m.streams) {
		tailResponse := logproto.TailResponse{
			Stream: &m.streams[m.index],
		}
		m.index += 1
		return &tailResponse, nil
	}

	return nil, errors.New("No more entries left")
}

func TestQuerier_Tail(t *testing.T) {
	testCases := []struct {
		tailClients map[string]logproto.Querier_TailClient
	}{
		{
			tailClients: map[string]logproto.Querier_TailClient{
				"1": &mockQuerierTailClient{
					streams: []logproto.Stream{
						{
							Labels: "foo=1",
							Entries: []logproto.Entry{
								{
									Timestamp: time.Unix(0, 0),
									Line: "foo line 1",
								},
								{
									Timestamp: time.Unix(0, 5),
									Line: "foo line 2",
								},
							},
						},
					},
				},
				"2": &mockQuerierTailClient{
					streams: []logproto.Stream{
						{
							Labels: "foo=1&bar=1",
							Entries: []logproto.Entry{
								{
									Timestamp: time.Unix(0, 0),
									Line: "foobar line 1",
								},
								{
									Timestamp: time.Unix(0, 1),
									Line: "foobar line 2",
								},
							},
						},
					},
				},
			},
		},
	}

	for _, testCase := range testCases {
		expected := TailResponse{
			Streams: []logproto.Stream{},
		}
		for i := range testCase.tailClients {
			tailClient := testCase.tailClients[i].(*mockQuerierTailClient)
			for _, stream := range tailClient.streams {
				for _, entry := range stream.Entries {
					expected.Streams = append(expected.Streams, logproto.Stream{Labels: stream.Labels, Entries: []logproto.Entry{entry}})
				}
			}
		}
		sort.Slice(expected.Streams, func(i, j int) bool {
			return expected.Streams[i].Entries[0].Timestamp.Before(expected.Streams[j].Entries[0].Timestamp)
		})
		tailer := newTailer(0, testCase.tailClients, func(from, to time.Time, labels string) (iterator iter.EntryIterator, e error) {
			return nil, nil
		}, func(strings []string) (clients map[string]logproto.Querier_TailClient, e error) {
			return nil, nil
		})
		responseChan := tailer.getResponseChan()
		response := <-responseChan
		assert.Equal(t, expected, *response)
		assert.NoError(t, tailer.close())
	}
}
