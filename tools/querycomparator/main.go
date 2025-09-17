package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sort"
	"time"

	"github.com/alecthomas/kingpin/v2"

	glog "github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine"
	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/storage/bucket/gcs"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"
)

var (
	bucket string
	orgID  string
)

func main() {
	app := kingpin.New("querycomparator", "A command-line tool to compare query results between two hosts.")
	addDebugCommand(app)
	kingpin.MustParse(app.Parse(os.Args[1:]))
}

func MustDataobjBucket() objstore.Bucket {
	bkt, err := gcs.NewBucketClient(context.Background(), gcs.Config{
		BucketName: bucket,
	}, "testing", glog.NewNopLogger(), nil)
	if err != nil {
		log.Fatal(err)
	}
	objBucket := objstore.NewPrefixedBucket(bkt, "dataobj")
	return objBucket
}

func MustIndexBucket() objstore.Bucket {
	bkt := MustDataobjBucket()
	return objstore.NewPrefixedBucket(bkt, "index/v0")
}

func addDebugCommand(app *kingpin.Application) {
	debugCmd := app.Command("debug", "Debug query results: Execute a single query against two Loki instances and compare the results")

	debugCmd.Flag("bucket", "Bucket name")
	debugCmd.Flag("org-id", "Org ID")
	debugCmd.Flag("start", "Start time")
	debugCmd.Flag("end", "End time")
	debugCmd.Flag("query", "Query")
	debugCmd.Flag("host-1", "Host 1").Default("localhost:3101")
	debugCmd.Flag("host-2", "Host 2").Default("localhost:3102")

	debugCmd.Action(func(c *kingpin.ParseContext) error {
		var start, end time.Time
		var startStr, endStr string
		var query string
		var host1, host2 string
		debugCmd.GetFlag("bucket").StringVar(&bucket)
		debugCmd.GetFlag("org-id").StringVar(&orgID)
		debugCmd.GetFlag("start").StringVar(&startStr)
		debugCmd.GetFlag("end").StringVar(&endStr)
		debugCmd.GetFlag("query").StringVar(&query)
		debugCmd.GetFlag("host-1").StringVar(&host1)
		debugCmd.GetFlag("host-2").StringVar(&host2)
		start, err := time.Parse(time.RFC3339, startStr)
		if err != nil {
			return err
		}
		end, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			return err
		}
		if start.After(end) {
			return fmt.Errorf("start time must be before end time")
		}
		err = doDebug(start, end, query, host1, host2, 0)
		if err != nil {
			return err
		}
		err = doMetastoreLookup(start, end, query)
		if err != nil {
			return err
		}
		err = doExecuteLocally(query, start, end, 0)
		if err != nil {
			return err
		}
		return nil
	})
}

func doDebug(start, end time.Time, query string, host1, host2 string, limit int) error {
	fmt.Println("Start: ", start.Format(time.RFC3339), "End: ", end.Format(time.RFC3339))
	hosts := []string{host1, host2}

	g, _ := errgroup.WithContext(context.Background())
	responses := make([]loghttp.QueryResponseData, len(hosts))

	for i, host := range hosts {
		g.Go(func() error {
			respData, err := doQuery(host, query, start, end, limit)
			if err != nil {
				return err
			}
			fmt.Printf("[host=%s] TotalEntriesReturned: %d\n", host, respData.Statistics.Summary.TotalEntriesReturned)
			responses[i] = respData
			return nil
		})
	}
	err := g.Wait()
	if err != nil {
		return err
	}

	responsesJson := make([][]string, len(responses))
	for i, response := range responses {
		var err error
		switch response.ResultType {
		case loghttp.ResultTypeStream:
			streams := response.Result.(loghttp.Streams)
			uniq := make(map[string]struct{})
			for _, stream := range streams {
				lbls, err := syntax.ParseLabels(stream.Labels.String())
				if err != nil {
					log.Fatal(err)
				}
				lblsMap := lbls.Map()
				newLbls := labels.FromMap(lblsMap)
				if _, ok := uniq[newLbls.String()]; ok {
					continue
				}
				uniq[newLbls.String()] = struct{}{}
				responsesJson[i] = append(responsesJson[i], newLbls.String())
			}
		default:
			return fmt.Errorf("Unsupported result type: %s", response.ResultType)
		}
		if err != nil {
			return err
		}
	}

	fmt.Println("Missing in first response (Chunks):")
	for _, lbls := range responsesJson[1] {
		if !slices.Contains(responsesJson[0], lbls) {
			fmt.Printf("	%+v\n", lbls)
		}
	}

	fmt.Println("Missing in second response (Dataobj):")
	valid := 0
	for _, lbls := range responsesJson[0] {
		if !slices.Contains(responsesJson[1], lbls) {
			fmt.Printf("	%+v\n", lbls)
			continue
		}
		valid++
	}
	fmt.Printf("matching streams: %d\n", valid)

	return nil
}

func doMetastoreLookup(start, end time.Time, query string) error {
	matchers, err := syntax.ParseMatchers(query, true)
	if err != nil {
		return err
	}

	metastoreStreams, err := askMetastoreForStreams(start, end, matchers)
	if err != nil {
		return err
	}
	fmt.Printf("Metastore streams:\n")
	sort.Slice(metastoreStreams, func(i, j int) bool {
		return metastoreStreams[i].String() < metastoreStreams[j].String()
	})
	uniq := make(map[string]struct{})
	for _, stream := range metastoreStreams {
		if _, ok := uniq[stream.String()]; ok {
			continue
		}
		uniq[stream.String()] = struct{}{}
	}
	for _, stream := range metastoreStreams {
		fmt.Printf("	%+v\n", stream)
	}

	sections, err := askMetastoreForSections(start, end, matchers)
	if err != nil {
		return err
	}
	fmt.Printf("Metastore sections:\n")
	for _, section := range sections {
		fmt.Printf("	%+v\n", section)
	}
	return nil
}

func doExecuteLocally(query string, start time.Time, end time.Time, limit int) error {
	fmt.Println("--------------------------------")
	fmt.Println("executing with V2 engine, locally")
	if limit == 0 {
		limit = 100
	}
	params, err := logql.NewLiteralParams(query, start, end, 1*time.Second, 1*time.Second, logproto.BACKWARD, uint32(limit), []string{}, nil)
	if err != nil {
		return err
	}
	result, err := doLocalV2Query(params)
	streams := result.Data.(logqlmodel.Streams)
	for _, stream := range streams {
		fmt.Printf("	%+v\n", stream.Labels)
	}
	return nil
}

func doQuery(host string, query string, start time.Time, end time.Time, limit int) (loghttp.QueryResponseData, error) {
	if limit == 0 {
		limit = 100
	}
	path := "/loki/api/v1/query_range"
	params := url.Values{
		"query":     {query},
		"start":     {fmt.Sprintf("%d", start.UnixNano())},
		"end":       {fmt.Sprintf("%d", end.UnixNano())},
		"direction": {"BACKWARD"},
		"limit":     {fmt.Sprintf("%d", limit)},
	}.Encode()
	url := "http://" + host + path + "?" + params

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return loghttp.QueryResponseData{}, err
	}
	req.Header.Add("X-Scope-OrgID", orgID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return loghttp.QueryResponseData{}, err
	}

	file, err := os.Create(fmt.Sprintf("host-%s.json", host))
	if err != nil {
		return loghttp.QueryResponseData{}, err
	}
	defer file.Close()
	teeReader := io.TeeReader(resp.Body, file)

	body, err := io.ReadAll(teeReader)
	if err != nil {
		return loghttp.QueryResponseData{}, err
	}

	var respData loghttp.QueryResponse
	err = json.Unmarshal(body, &respData)
	if err != nil {
		return loghttp.QueryResponseData{}, fmt.Errorf("unmarshalling response: %s %w", string(body), err)
	}
	return respData.Data, nil
}

func askMetastoreForStreams(start, end time.Time, matchers []*labels.Matcher) ([]*labels.Labels, error) {
	ctx := user.InjectOrgID(context.Background(), orgID)
	metastore := metastore.NewObjectMetastore(MustIndexBucket(), glog.NewNopLogger(), nil)
	streams, err := metastore.Streams(ctx, start, end, matchers...)
	if err != nil {
		return nil, err
	}
	return streams, nil
}

func askMetastoreForSections(start, end time.Time, matchers []*labels.Matcher) ([]*metastore.DataobjSectionDescriptor, error) {
	ctx := user.InjectOrgID(context.Background(), orgID)
	metastore := metastore.NewObjectMetastore(MustIndexBucket(), glog.NewLogfmtLogger(os.Stderr), nil)
	sections, err := metastore.Sections(ctx, start, end, matchers, nil)
	if err != nil {
		return nil, err
	}
	return sections, nil
}

func doLocalV2Query(params logql.LiteralParams) (logqlmodel.Result, error) {
	ctx := user.InjectOrgID(context.Background(), orgID)
	qe := engine.New(logql.EngineOpts{
		EnableV2Engine: true,
		BatchSize:      512,
	}, metastore.Config{
		IndexStoragePrefix: "index/v0",
	}, MustDataobjBucket(), logql.NoLimits, prometheus.DefaultRegisterer, glog.NewLogfmtLogger(os.Stderr))
	query := qe.Query(params)
	return query.Exec(ctx)
}
