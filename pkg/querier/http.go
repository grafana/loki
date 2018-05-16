package querier

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/grafana/logish/pkg/logproto"
)

const (
	defaultQueryLimit = 100
)

func (q *Querier) QueryHandler(w http.ResponseWriter, r *http.Request) {
	query := r.FormValue("query")

	limit := defaultQueryLimit
	if limitStr := r.FormValue("limit"); limitStr != "" {
		var err error
		limit, err = strconv.Atoi(limitStr)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	result, err := q.Query(r.Context(), query, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := json.NewEncoder(w).Encode(result); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
}

func (q *Querier) Query(ctx context.Context, query string, limit int) (*logproto.QueryResponse, error) {
	req := &logproto.QueryRequest{
		Query: query,
	}
	clients, err := q.forAllIngesters(func(client logproto.QuerierClient) (interface{}, error) {
		return client.Query(ctx, req)
	})
	if err != nil {
		return nil, err
	}

	iterators := make([]EntryIterator, len(clients))
	for i := range clients {
		iterators[i] = newQueryClientIterator(clients[i].(logproto.Querier_QueryClient))
	}
	iterator := NewHeapIterator(iterators)
	defer iterator.Close()

	return ReadBatch(iterator, limit)
}

func ReadBatch(i EntryIterator, size int) (*logproto.QueryResponse, error) {
	streams := map[string]*logproto.Stream{}

	for respSize := 0; respSize < size && i.Next(); respSize++ {
		labels, entry := i.Labels(), i.Entry()
		stream, ok := streams[labels]
		if !ok {
			stream = &logproto.Stream{
				Labels: labels,
			}
			streams[labels] = stream
		}
		stream.Entries = append(stream.Entries, entry)
	}

	result := logproto.QueryResponse{
		Streams: make([]*logproto.Stream, len(streams)),
	}
	for _, stream := range streams {
		result.Streams = append(result.Streams, stream)
	}
	return &result, i.Error()
}
