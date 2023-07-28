package queryrange

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel/stats"

	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/util/marshal"
)

func TestSeriesIdentifierViewHash(t *testing.T) {
	identifier := &logproto.SeriesIdentifier{
		Labels: map[string]string{
			"foo": "bar",
			"baz": "woof",
		},
	}

	data, err := identifier.Marshal()
	require.NoError(t, err)

	view := &SeriesIdentifierView{buffer: data}

	b := make([]byte, 0, 1024)
	keyLabelPairs := make([]string, 0)
	var actual uint64
	actual, keyLabelPairs, err = view.Hash(b, keyLabelPairs)
	require.NoError(t, err)
	require.ElementsMatch(t, keyLabelPairs, []string{"baz\xffwoof\xff", "foo\xffbar\xff"})

	expected, _ := identifier.Hash(b, keyLabelPairs)
	require.Equal(t, expected, actual)
}
func TestSeriesIdentifierViewForEachLabel(t *testing.T) {
	identifier := &logproto.SeriesIdentifier{
		Labels: map[string]string{
			"foo": "bar",
			"baz": "woof",
		},
	}

	data, err := identifier.Marshal()
	require.NoError(t, err)

	view := &SeriesIdentifierView{buffer: data}

	s := make([]string, 0)
	err = view.ForEachLabel(func(name, value string) error {
		s = append(s, name, value)
		return nil
	})
	require.NoError(t, err)

	require.ElementsMatch(t, s, []string{"baz", "woof", "foo", "bar"})
}

func TestSeriesResponseViewForEach(t *testing.T) {
	response := &LokiSeriesResponse{
		Data: []logproto.SeriesIdentifier{
			{
				Labels: map[string]string{"i": "1", "baz": "woof"},
			},
			{
				Labels: map[string]string{"i": "2", "foo": "bar"},
			},
		},
	}

	data, err := response.Marshal()
	require.NoError(t, err)

	view := &LokiSeriesResponseView{buffer: data}
	actualHashes := make([]uint64, 0)
	err = view.ForEachSeries(func(s *SeriesIdentifierView) error {
		b := make([]byte, 0, 1024)
		keyLabelPairs := make([]string, 0)
		hash, _, err := s.Hash(b, keyLabelPairs)
		if err != nil {
			return err
		}
		actualHashes = append(actualHashes, hash)
		return nil
	})
	require.NoError(t, err)

	expectedHashes := make([]uint64, 0)
	for _, id := range response.Data {
		b := make([]byte, 0, 1024)
		keyLabelPairs := make([]string, 0)
		hash, _ := id.Hash(b, keyLabelPairs)
		expectedHashes = append(expectedHashes, hash)
	}
	require.ElementsMatch(t, expectedHashes, actualHashes)
}

func TestMergedViewDeduplication(t *testing.T) {
	responses := []*LokiSeriesResponse{
		{
			Data: []logproto.SeriesIdentifier{
				{
					Labels: map[string]string{"i": "1", "baz": "woof"},
				},
				{
					Labels: map[string]string{"i": "2", "foo": "bar"},
				},
			},
		},
		{
			Data: []logproto.SeriesIdentifier{
				{
					Labels: map[string]string{"i": "3", "baz": "woof"},
				},
				{
					Labels: map[string]string{"i": "2", "foo": "bar"},
				},
			},
		},
	}

	view := &MergedSeriesResponseView{}
	for _, r := range responses {
		data, err := r.Marshal()
		require.NoError(t, err)

		view.responses = append(view.responses, &LokiSeriesResponseView{data})
	}

	count := 0
	err := view.ForEachUniqueSeries(func(s *SeriesIdentifierView) error {
		count++
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 3, count)
}

func TestMergedViewJSON(t *testing.T) {
	var b strings.Builder
	response := &LokiSeriesResponse{
		Data: []logproto.SeriesIdentifier{
			{
				Labels: map[string]string{"i": "1", "baz": "woof"},
			},
			{
				Labels: map[string]string{"i": "2", "foo": "bar"},
			},
			{
				Labels: map[string]string{"i": "3", "baz": "woof"},
			},
		},
	}

	view := &MergedSeriesResponseView{}
	data, err := response.Marshal()
	require.NoError(t, err)
	view.responses = append(view.responses, &LokiSeriesResponseView{data})

	err = WriteSeriesResponseViewJSON(view, &b)
	require.NoError(t, err)
	actual := b.String()
	b.Reset()

	result := logproto.SeriesResponse{
		Series: response.Data,
	}
	err = marshal.WriteSeriesResponseJSON(result, &b)
	require.NoError(t, err)
	expected := b.String()

	require.JSONEq(t, expected, actual)
}

func Benchmark_DecodeAndMergeSeriesResponses(b *testing.B) {
	var err error
	data := make([][]byte, 100)
	for i := range data {
		r := &LokiSeriesResponse{
			Status:     "200",
			Version:    1,
			Statistics: stats.Result{},
			Data:       generateSeries(),
		}
		data[i], err = r.Marshal()
		require.NoError(b, err)
	}

	view := &MergedSeriesResponseView{}
	for _, d := range data {
		view.responses = append(view.responses, &LokiSeriesResponseView{buffer: d})
	}
	var builder strings.Builder

	b.ReportAllocs()
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		builder.Reset()
		err := WriteSeriesResponseViewJSON(view, &builder)
		require.Nil(b, err)
	}
}

func Benchmark_DecodeMergeEncodeCycle(b *testing.B) {
	// Setup HTTP responses from querier with protobuf encoding.
	u := &url.URL{Path: "/loki/api/v1/series"}
	httpReq := &http.Request{
		Method:     "GET",
		RequestURI: u.String(),
		URL:        u,
		Header: http.Header{
			"Accept": []string{ProtobufType},
		},
	}
	qreq, err := DefaultCodec.DecodeRequest(context.Background(), httpReq, []string{})
	require.NoError(b, err)

	responses := make([]*LokiSeriesResponse, 100)
	for i := range responses {
		responses[i] = &LokiSeriesResponse{
			Status:     "200",
			Version:    1,
			Statistics: stats.Result{},
			Data:       generateSeries(),
		}
	}

	httpResponses := make([]*http.Response, 0)
	for _, r := range responses {
		resp, err := DefaultCodec.EncodeResponse(context.Background(), httpReq, r)
		require.NoError(b, err)
		httpResponses = append(httpResponses, resp)
	}

	qresps := make([]queryrangebase.Response, 0, 100)

	b.ReportAllocs()
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		// Decode
		qresps = qresps[:0]
		for _, httpResp := range httpResponses {
			qresp, err := DefaultCodec.DecodeResponse(context.Background(), httpResp, qreq)
			require.NoError(b, err)
			qresps = append(qresps, qresp)
		}

		// Merge
		result, _ := DefaultCodec.MergeResponse(qresps...)

		// Encode
		_, err = DefaultCodec.EncodeResponse(context.Background(), httpReq, result)
		require.NoError(b, err)
	}

}
