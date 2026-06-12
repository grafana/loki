package labelaccess

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/schema"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/labelaccess/types"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	chunk_local "github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	storage_config "github.com/grafana/loki/v3/pkg/storage/config"
	loki_util "github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
)

var (
	start      = model.Time(0)
	tenantName = "david"
)

func TestWrapperLabelValuesForMetricName(t *testing.T) {
	util_log.Logger = log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	ctx := context.Background()

	chunks := []chunk.Chunk{
		newChunk(logproto.Stream{
			Labels:  `{env="dev", classification="confidential"}`,
			Entries: []logproto.Entry{{Timestamp: time.Unix(0, 1), Line: "1"}},
		}),
		newChunk(logproto.Stream{
			Labels:  `{env="dev", classification="secret"}`,
			Entries: []logproto.Entry{{Timestamp: time.Unix(0, 2), Line: "3"}},
		}),
		newChunk(logproto.Stream{
			Labels:  `{env="prod", classification="secret"}`,
			Entries: []logproto.Entry{{Timestamp: time.Unix(0, 2), Line: "4"}},
		}),
		newChunk(logproto.Stream{
			Labels:  `{env="prod", classification="secret"}`,
			Entries: []logproto.Entry{{Timestamp: time.Unix(0, 3), Line: "4"}},
		}),
		newChunk(logproto.Stream{
			Labels:  `{env="prod", classification="notsecret"}`,
			Entries: []logproto.Entry{{Timestamp: time.Unix(0, 3), Line: "5"}},
		}),
		newChunk(logproto.Stream{
			Labels:  `{env="prod-confidential", classification="confidential"}`,
			Entries: []logproto.Entry{{Timestamp: time.Unix(0, 3), Line: "6"}},
		}),
	}
	m := getLocalStore(t)
	err := m.Put(ctx, chunks)
	require.NoError(t, err)
	w := WrapStore(m)

	for _, tc := range []struct {
		name          string
		labelPolicies []*types.LabelPolicy
		labelValues   map[string][]string
		extraMatchers []*labels.Matcher
	}{
		{
			name: "dev-only",
			labelPolicies: []*types.LabelPolicy{
				{
					Selector: []*types.LabelMatcher{
						{
							Type:  types.LABEL_MATCHER_TYPE_EQ,
							Name:  "env",
							Value: "dev",
						},
					},
				},
			},
			labelValues: map[string][]string{
				"env":            {"dev"},
				"classification": {"secret", "confidential"},
			},
		},
		{
			name: "not-confidential-not-secret",
			labelPolicies: []*types.LabelPolicy{
				{
					Selector: []*types.LabelMatcher{
						{
							Type:  types.LABEL_MATCHER_TYPE_NEQ,
							Name:  "classification",
							Value: "confidential",
						},
						{
							Type:  types.LABEL_MATCHER_TYPE_NEQ,
							Name:  "classification",
							Value: "secret",
						},
					},
				},
			},
			labelValues: map[string][]string{
				"env":            {"prod"},
				"classification": {"notsecret"},
			},
		},
		{
			name: "use all matchers",
			labelPolicies: []*types.LabelPolicy{
				{
					Selector: []*types.LabelMatcher{
						{
							Type:  types.LABEL_MATCHER_TYPE_RE,
							Name:  "env",
							Value: ".*",
						},
					},
				},
			},
			labelValues: map[string][]string{
				"env": {"dev"},
			},
			extraMatchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "env", "dev"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			matchers := LabelPolicySet{}
			matchers[tenantName] = tc.labelPolicies

			ctx = InjectLabelMatchersContext(ctx, matchers)
			ctx = user.InjectOrgID(ctx, tenantName)

			from, through := loki_util.RoundToMilliseconds(time.Unix(0, 0), time.Unix(10, 0))

			for labelName, expLabelValues := range tc.labelValues {
				actualLabelValues, err := w.LabelValuesForMetricName(ctx, tenantName, from, through, "logs", labelName, tc.extraMatchers...)
				require.NoError(t, err)
				require.ElementsMatch(t, expLabelValues, actualLabelValues)
			}
		})
	}
}

// helper methods from loki/pkg/storage
func getLocalStore(t *testing.T) storage.Store {
	limits, err := validation.NewOverrides(validation.Limits{
		MaxQueryLength: model.Duration(6000 * time.Hour),
	}, nil)
	if err != nil {
		panic(err)
	}

	storeConfig := storage.Config{
		BoltDBConfig:      chunk_local.BoltDBConfig{Directory: t.TempDir()},
		FSConfig:          chunk_local.FSConfig{Directory: t.TempDir()},
		MaxChunkBatchSize: 10,
	}

	schemaConfig := storage_config.SchemaConfig{
		Configs: []storage_config.PeriodConfig{
			{
				From:       storage_config.DayTime{Time: start},
				IndexType:  "boltdb",
				ObjectType: "filesystem",
				Schema:     "v9",
				IndexTables: storage_config.IndexPeriodicTableConfig{
					PeriodicTableConfig: storage_config.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 168,
					}},
			},
		},
	}

	clientMetrics := storage.NewClientMetrics()
	store, err := storage.NewStore(
		storeConfig,
		storage_config.ChunkStoreConfig{},
		schemaConfig, limits, clientMetrics, nil, util_log.Logger, "namespace")
	if err != nil {
		panic(err)
	}

	return store
}

func newChunk(stream logproto.Stream) chunk.Chunk {
	lbs, err := syntax.ParseLabels(stream.Labels)
	if err != nil {
		panic(err)
	}
	if schema.NewMetadataFromLabels(lbs).Name == "" {
		builder := labels.NewBuilder(lbs)
		schema.Metadata{Name: "logs"}.SetToLabels(builder)
		lbs = builder.Labels()
	}
	from, through := loki_util.RoundToMilliseconds(stream.Entries[0].Timestamp, stream.Entries[len(stream.Entries)-1].Timestamp)
	chk := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, compression.GZIP, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, 256*1024, 0)
	for _, e := range stream.Entries {
		_, _ = chk.Append(&e)
	}
	chk.Close()
	c := chunk.NewChunk(tenantName, client.Fingerprint(lbs), lbs, chunkenc.NewFacade(chk, 0, 0), from, through)
	// force the checksum creation
	if err := c.Encode(); err != nil {
		panic(err)
	}
	return c
}
