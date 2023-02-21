package coprocessor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"github.com/grafana/loki/pkg/coprocessor/proto"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/grafana/loki/pkg/loghttp"
	lokiutil "github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/build"
)

var timeout = 2 * time.Minute
var UserAgent = fmt.Sprintf("lokiObserver/%s", build.Version)
var contentType = "application/x-protobuf"

const maxErrMsgLen = 1024

type querierObserver struct {
	endpoint string
	client   *http.Client
}

func NewQuerierObserver(URL *url.URL) QuerierObserver {
	endpoint := URL.String()

	return &querierObserver{
		endpoint: endpoint,
		client:   &http.Client{},
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

	ctx, cancel := context.WithTimeout(ctx, timeout)
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
