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

type mockQuerier_TailClient struct {
	streams []logproto.Stream
	index   int
}

func (m mockQuerier_TailClient) Header() (metadata.MD, error) {
	return nil, nil
}

func (m mockQuerier_TailClient) Trailer() metadata.MD {
	return nil
}

func (m mockQuerier_TailClient) CloseSend() error {
	return nil
}

func (m mockQuerier_TailClient) Context() context.Context {
	return nil
}

func (m mockQuerier_TailClient) SendMsg(i interface{}) error {
	return nil
}

func (m mockQuerier_TailClient) RecvMsg(i interface{}) error {
	return nil
}

func (m *mockQuerier_TailClient) Recv() (*logproto.TailResponse, error) {
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
		expected    TailResponse
	}{
		{
			tailClients: map[string]logproto.Querier_TailClient{
				"1": &mockQuerier_TailClient{
					streams: []logproto.Stream{
						{
							"foo=1",
							[]logproto.Entry{
								{
									time.Unix(0, 0),
									"foo line 1",
								},
								{
									time.Unix(0, 5),
									"foo line 2",
								},
							},
						},
					},
				},
				"2": &mockQuerier_TailClient{
					streams: []logproto.Stream{
						{
							"foo=1&bar=1",
							[]logproto.Entry{
								{
									time.Unix(0, 0),
									"foobar line 1",
								},
								{
									time.Unix(0, 1),
									"foobar line 2",
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
			tailClient := testCase.tailClients[i].(*mockQuerier_TailClient)
			for _, stream := range tailClient.streams {
				for _, entry := range stream.Entries {
					expected.Streams = append(expected.Streams, logproto.Stream{stream.Labels, []logproto.Entry{entry}})
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
