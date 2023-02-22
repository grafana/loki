package coprocessor

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/prometheus/common/config"

	"github.com/grafana/loki/pkg/coprocessor/proto"
	"github.com/grafana/loki/pkg/loghttp"
	lokiutil "github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/build"
)

var UserAgent = fmt.Sprintf("lokiObserver/%s", build.Version)
var contentType = "application/x-protobuf"

const maxErrMsgLen = 1024

type Config struct {
	PreQuery PreQueryConfig `yaml:"pre_query"`
}

// RegisterFlags register flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.PreQuery.RegisterFlags(f)
}

type PreQueryConfig struct {
	URL     *config.URL   `yaml:"url"`
	Timeout time.Duration `yaml:"timeout"`
}

// RegisterFlags register flags.
func (cfg *PreQueryConfig) RegisterFlags(f *flag.FlagSet) {
	f.DurationVar(&cfg.Timeout, "coprocessor.pre-query-timeout", 2*time.Minute, "timeout.")
}

type querierObserver struct {
	endpoint string
	client   *http.Client
	timeout  time.Duration
}

func NewQuerierObserver(preQueryConfig PreQueryConfig) QuerierObserver {
	endpoint := preQueryConfig.URL.String()

	return &querierObserver{
		endpoint: endpoint,
		client:   &http.Client{},
		timeout:  preQueryConfig.Timeout,
	}
}
func (q *querierObserver) PreQuery(ctx context.Context, rangeQuery loghttp.RangeQuery) (pass bool, err error) {

	queryRequest := proto.QueryPreQueryRequest{
		Selector: rangeQuery.Query,
		Start:    rangeQuery.Start,
		End:      rangeQuery.End,
		Limit:    rangeQuery.Limit,
		Shards:   rangeQuery.Shards,
	}

	marshal, err := queryRequest.Marshal()
	if err != nil {
		return false, err
	}

	ctx, cancel := context.WithTimeout(ctx, q.timeout)
	defer cancel()
	req, err := http.NewRequest("POST", q.endpoint, bytes.NewReader(marshal))
	if err != nil {
		return false, err
	}
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", UserAgent)

	resp, err := q.client.Do(req)
	if err != nil {
		return false, err
	}
	defer lokiutil.LogError("closing response body", resp.Body.Close)

	if resp.StatusCode/100 != 2 {
		scanner := bufio.NewScanner(io.LimitReader(resp.Body, maxErrMsgLen))
		line := ""
		if scanner.Scan() {
			line = scanner.Text()
		}
		err = fmt.Errorf("server returned HTTP status %s (%d): %s", resp.Status, resp.StatusCode, line)
	}

	result, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	res := proto.QueryPreQueryResponse{}
	err = res.Unmarshal(result)
	if err != nil {
		return false, err
	}

	if res.Pass {
		return true, nil
	}

	return false, nil
}
