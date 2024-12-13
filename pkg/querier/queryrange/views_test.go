package queryrange

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"

	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util/marshal"
)

func TestGetLokiSeriesResponse(t *testing.T) {
	p := QueryResponse{
		Response: &QueryResponse_Series{
			Series: &LokiSeriesResponse{
				Status: "success",
				Data: []logproto.SeriesIdentifier{
					{
						Labels: []logproto.SeriesIdentifier_LabelsEntry{
							{Key: "foo", Value: "bar"},
							{Key: "baz", Value: "woof"},
						},
					},
				},
			}},
	}
	buf, err := p.Marshal()
	require.NoError(t, err)

	view, err := GetLokiSeriesResponseView(buf)
	require.NoError(t, err)
	actual := make([]string, 0)
	err = view.ForEachSeries(func(identifier *SeriesIdentifierView) error {
		return identifier.ForEachLabel(func(name, value string) error {
			actual = append(actual, name+value)
			return nil
		})
	})
	require.NoError(t, err)
	require.ElementsMatch(t, actual, []string{"foobar", "bazwoof"})
}

func TestSeriesIdentifierViewHash(t *testing.T) {
	identifier := &logproto.SeriesIdentifier{
		Labels: []logproto.SeriesIdentifier_LabelsEntry{
			{Key: "foo", Value: "bar"},
			{Key: "baz", Value: "woof"},
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

	expected := identifier.Hash(b)
	require.Equal(t, expected, actual)
}
func TestSeriesIdentifierViewForEachLabel(t *testing.T) {
	identifier := &logproto.SeriesIdentifier{
		Labels: []logproto.SeriesIdentifier_LabelsEntry{
			{Key: "foo", Value: "bar"},
			{Key: "baz", Value: "woof"},
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
				Labels: []logproto.SeriesIdentifier_LabelsEntry{
					{Key: "i", Value: "1"},
					{Key: "baz", Value: "woof"},
				},
			},
			{
				Labels: []logproto.SeriesIdentifier_LabelsEntry{
					{Key: "i", Value: "2"},
					{Key: "foo", Value: "bar"},
				},
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
		hash := id.Hash(b)
		expectedHashes = append(expectedHashes, hash)
	}
	require.ElementsMatch(t, expectedHashes, actualHashes)
}

func TestMergedViewDeduplication(t *testing.T) {
	responses := []*LokiSeriesResponse{
		{
			Data: []logproto.SeriesIdentifier{
				{
					Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "i", Value: "1"},
						{Key: "baz", Value: "woof"},
					},
				},
				{
					Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "i", Value: "2"},
						{Key: "foo", Value: "bar"},
					},
				},
			},
		},
		{
			Data: []logproto.SeriesIdentifier{
				{
					Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "i", Value: "3"},
						{Key: "baz", Value: "woof"},
					},
				},
				{
					Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "i", Value: "2"},
						{Key: "foo", Value: "bar"},
					},
				},
			},
		},
	}

	view := &MergedSeriesResponseView{}
	for _, r := range responses {
		data, err := r.Marshal()
		require.NoError(t, err)

		view.responses = append(view.responses, &LokiSeriesResponseView{buffer: data, headers: nil})
	}

	count := 0
	err := view.ForEachUniqueSeries(func(_ *SeriesIdentifierView) error {
		count++
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 3, count)
}

func TestMergedViewMaterialize(t *testing.T) {
	responses := []*LokiSeriesResponse{
		{
			Data: []logproto.SeriesIdentifier{
				{
					Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "i", Value: "1"},
						{Key: "baz", Value: "woof"},
					},
				},
				{
					Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "i", Value: "2"},
						{Key: "foo", Value: "bar"},
					},
				},
			},
		},
		{
			Data: []logproto.SeriesIdentifier{
				{
					Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "i", Value: "3"},
						{Key: "baz", Value: "woof"},
					},
				},
				{
					Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "i", Value: "2"},
						{Key: "foo", Value: "bar"},
					},
				},
			},
		},
	}

	view := &MergedSeriesResponseView{}
	for _, r := range responses {
		data, err := r.Marshal()
		require.NoError(t, err)

		view.responses = append(view.responses, &LokiSeriesResponseView{buffer: data, headers: nil})
	}

	mat, err := view.Materialize()
	require.NoError(t, err)
	require.Len(t, mat.Data, 3)
	series := make([]string, 0)
	for _, d := range mat.Data {
		l := make([]labels.Label, 0, len(d.Labels))
		for _, p := range d.Labels {
			l = append(l, labels.Label{Name: p.Key, Value: p.Value})
		}
		series = append(series, labels.Labels(l).String())
	}
	expected := []string{`{i="1", baz="woof"}`, `{i="3", baz="woof"}`, `{i="2", foo="bar"}`}
	require.ElementsMatch(t, series, expected)
}

func TestMergedViewJSON(t *testing.T) {
	var b strings.Builder
	response := &LokiSeriesResponse{
		Data: []logproto.SeriesIdentifier{
			{
				Labels: []logproto.SeriesIdentifier_LabelsEntry{
					{Key: "i", Value: "1"},
					{Key: "baz", Value: "woof"},
				},
			},
			{
				Labels: []logproto.SeriesIdentifier_LabelsEntry{
					{Key: "i", Value: "2"},
					{Key: "foo", Value: "bar"},
				},
			},
			{
				Labels: []logproto.SeriesIdentifier_LabelsEntry{
					{Key: "i", Value: "3"},
					{Key: "baz", Value: "woof"},
				},
			},
		},
	}

	view := &MergedSeriesResponseView{}
	data, err := response.Marshal()
	require.NoError(t, err)
	view.responses = append(view.responses, &LokiSeriesResponseView{buffer: data, headers: nil})

	err = WriteSeriesResponseViewJSON(view, &b)
	require.NoError(t, err)
	actual := b.String()
	b.Reset()

	err = marshal.WriteSeriesResponseJSON(response.Data, &b)
	require.NoError(t, err)
	expected := b.String()

	require.JSONEq(t, expected, actual)
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
	readers := make([]*bytes.Reader, 0)
	for _, r := range responses {
		resp, err := DefaultCodec.EncodeResponse(context.Background(), httpReq, r)
		require.NoError(b, err)
		buf, err := io.ReadAll(resp.Body)
		require.Nil(b, err)
		reader := bytes.NewReader(buf)
		resp.Body = io.NopCloser(reader)
		readers = append(readers, reader)

		httpResponses = append(httpResponses, resp)
	}

	// Originally the responses were encoded using protobuf. Demand JSON
	// now.
	httpReq.Header.Del("Accept")
	httpReq.Header.Add("Accept", JSONType)

	qresps := make([]queryrangebase.Response, 0, 100)

	b.ReportAllocs()
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		// Decode
		qresps = qresps[:0]
		for i, httpResp := range httpResponses {
			_, _ = readers[i].Seek(0, io.SeekStart)
			qresp, err := DefaultCodec.DecodeResponse(context.Background(), httpResp, qreq)
			require.NoError(b, err)
			qresps = append(qresps, qresp)
		}

		// Merge
		result, _ := DefaultCodec.MergeResponse(qresps...)

		// Encode
		httpRes, err := DefaultCodec.EncodeResponse(context.Background(), httpReq, result)
		require.NoError(b, err)
		require.Equal(b, "application/json; charset=UTF-8", httpRes.Header.Get("Content-Type"))
	}

}
