package remote

import (
	"context"
	"github.com/pkg/errors"
	"github.com/prometheus/common/config"
	"strings"
	"time"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/querier"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
)

// ReadConfig is the configuration for reading from remote storage.
// code from /github.com/prometheus/prometheus/config/config.go:868
type ReadConfig struct {
	URL   *config.URL `yaml:"url"`
	Name  string      `yaml:"name,omitempty"`
	OrgID string      `yaml:"orgID,omitempty"`

	HTTPClientConfig config.HTTPClientConfig `yaml:",inline"`

	//todo:@liguozhong support http herder
	Headers map[string]string `yaml:"headers,omitempty"`
	//todo:@liguozhong support RemoteTimeout
	RemoteTimeout time.Duration `yaml:"remote_timeout,omitempty"`
}

func NewQuerier(name string, remoteReadConfig ReadConfig) (querier.Querier, error) {
	client := &client.DefaultClient{
		OrgID:   remoteReadConfig.OrgID,
		Address: remoteReadConfig.URL.String(),
		TLSConfig: config.TLSConfig{
			InsecureSkipVerify: remoteReadConfig.HTTPClientConfig.TLSConfig.InsecureSkipVerify,
		},
	}
	if remoteReadConfig.HTTPClientConfig.BasicAuth != nil {
		client.Username = remoteReadConfig.HTTPClientConfig.BasicAuth.Username
		client.Password = strings.TrimSpace(string(remoteReadConfig.HTTPClientConfig.BasicAuth.Password))
	}

	return &Querier{
		client: client,
		name:   name,
	}, nil
}

type Querier struct {
	client *client.DefaultClient
	codec  queryrangebase.Codec
	name   string
}

func (q Querier) SelectLogs(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, error) {
	response, err := q.client.QueryRange(params.Selector, int(params.Limit), params.Start, params.End, params.Direction, 0, 0, false)
	if err != nil {
		return nil, err
	}
	if response.Status != loghttp.QueryStatusSuccess {
		return nil, errors.Errorf("remote read Querier selectLogs fail,response.Status %v", response.Status)
	}

	streams, ok := response.Data.Result.(loghttp.Streams)
	if !ok {
		return nil, errors.New("remote read Querier selectLogs fail,value cast (loghttp.Streams) fail")
	}
	return iter.NewStreamsIterator(streams.ToProto(), params.Direction), nil
}

func (q Querier) SelectSamples(ctx context.Context, params logql.SelectSampleParams) (iter.SampleIterator, error) {
	panic("implement me")
}

func (q Querier) Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	//TODO implement me
	return &logproto.LabelResponse{}, nil
}

func (q Querier) Series(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	//TODO implement me
	return &logproto.SeriesResponse{}, nil
}

func (q Querier) Tail(ctx context.Context, req *logproto.TailRequest) (*querier.Tailer, error) {
	//TODO implement me
	panic("implement me")
}

func (q Querier) IndexStats(ctx context.Context, req *loghttp.RangeQuery) (*stats.Stats, error) {
	//TODO implement me
	return &stats.Stats{}, nil
}
