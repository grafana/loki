package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// addMetastoreCommand adds the metastore command to the application
func addMetastoreCommand(app *kingpin.Application) {
	var cfg Config

	cmd := app.Command("metastore", "Query metastore for stream information using remote storage bucket")
	cmd.Flag("bucket", "Remote bucket name").Required().StringVar(&cfg.Bucket)
	cmd.Flag("org-id", "Organization ID").Required().StringVar(&cfg.OrgID)
	cmd.Flag("start", "Start time (RFC3339 format)").Required().StringVar(&cfg.Start)
	cmd.Flag("end", "End time (RFC3339 format)").Required().StringVar(&cfg.End)
	cmd.Flag("query", "LogQL query to analyze").Required().StringVar(&cfg.Query)

	cmd.Action(func(_ *kingpin.ParseContext) error {
		orgID = cfg.OrgID

		parsed, err := parseTimeConfig(&cfg)
		if err != nil {
			return err
		}

		bucket := MustGCSDataobjBucket(cfg.Bucket)

		params, err := logql.NewLiteralParams(cfg.Query, parsed.StartTime, parsed.EndTime, 0, 0, logproto.BACKWARD, 10, nil, nil)
		if err != nil {
			return err
		}

		return queryMetastore(params, bucket)
	})
}

// queryMetastore queries the metastore for stream sections
func queryMetastore(params logql.LiteralParams, bucket objstore.Bucket) error {
	query := params.QueryString()
	closeIdx := strings.Index(query, "}")
	streamMatchers, err := syntax.ParseMatchers(query[:closeIdx+1], true)
	if err != nil {
		return err
	}

	sections, err := getSections(bucket, params.Start(), params.End(), streamMatchers)
	if err != nil {
		return err
	}
	level.Info(logger).Log("msg", "metastore sections found", "count", len(sections))
	for _, section := range sections {
		level.Info(logger).Log("msg", "metastore section", "section", fmt.Sprintf("%+v", section))
	}
	return nil
}

// getSections queries the metastore for dataobject sections matching the query selector
// Currently, it does not pass structured metadata predicates
func getSections(bucket objstore.Bucket, start, end time.Time, streamMatchers []*labels.Matcher) ([]*metastore.DataobjSectionDescriptor, error) {
	ctx := user.InjectOrgID(context.Background(), orgID)
	ms := metastore.NewObjectMetastore(
		bucket,
		metastore.Config{IndexStoragePrefix: indexStoragePrefix},
		log.NewLogfmtLogger(os.Stderr),
		metastore.NewObjectMetastoreMetrics(nil),
	)
	sectionsResp, err := ms.Sections(ctx, metastore.SectionsRequest{Start: start, End: end, Matchers: streamMatchers})
	if err != nil {
		return nil, err
	}
	return sectionsResp.Sections, nil
}
