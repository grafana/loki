package tsdb

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	index_tsdb "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	tsdbindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/sectionref"
)

type tsdbBuilder interface {
	Append(ctx context.Context, obj *dataobj.Object, objectPath string) error
	Store(ctx context.Context) error
}

func newTSDBBuilder(nodeName string, bkt objstore.Bucket) tsdbBuilder {
	return &dataobjTSDBBuilder{
		nodeName:    nodeName,
		bkt:         bkt,
		dayBuilders: make(map[time.Time]*index_tsdb.Builder),
	}
}

type dataobjTSDBBuilder struct {
	nodeName    string
	bkt         objstore.Bucket
	dayBuilders map[time.Time]*index_tsdb.Builder
}

type streamKey struct {
	tenant   string
	streamID int64
}

type sectionStats struct {
	minTime int64
	maxTime int64
	bytes   int64
	entries uint32
	set     bool
}

const dayWindow = 24 * time.Hour

// Append collects stream labels and section metas from the object, partitions
// them by day, and adds the series to retained per-day index_tsdb.Builders.
// The builders are accumulated across calls; call Store to flush them.
func (b *dataobjTSDBBuilder) Append(ctx context.Context, obj *dataobj.Object, objectPath string) error {
	streamLabels, err := collectStreamLabels(ctx, obj)
	if err != nil {
		return err
	}

	streamSectionMetas, err := collectSectionMetas(ctx, obj, objectPath)
	if err != nil {
		return err
	}
	if len(streamSectionMetas) == 0 {
		return nil
	}

	// Partition metas into daily buckets. A meta that spans a day boundary
	// is included in every day it overlaps.
	dayBuckets := make(map[time.Time]map[streamKey][]sectionref.SectionMeta)
	for key, metas := range streamSectionMetas {
		for _, meta := range metas {
			dayStart := time.UnixMilli(meta.MinTime).UTC().Truncate(dayWindow)
			dayEnd := time.UnixMilli(meta.MaxTime).UTC()
			for d := dayStart; !d.After(dayEnd); d = d.Add(dayWindow) {
				if dayBuckets[d] == nil {
					dayBuckets[d] = make(map[streamKey][]sectionref.SectionMeta)
				}
				dayBuckets[d][key] = append(dayBuckets[d][key], meta)
			}
		}
	}

	for day, bucket := range dayBuckets {
		builder, ok := b.dayBuilders[day]
		if !ok {
			builder = index_tsdb.NewBuilder(tsdbindex.FormatV3)
			b.dayBuilders[day] = builder
		}

		for key, metas := range bucket {
			lbls, ok := streamLabels[key]
			if !ok {
				return fmt.Errorf("missing stream labels for tenant=%q streamID=%d", key.tenant, key.streamID)
			}

			fp := model.Fingerprint(labels.StableHash(lbls))
			if err := builder.AddSeriesWithSectionRefs(lbls, fp, metas); err != nil {
				return fmt.Errorf("adding stream to tsdb builder: %w", err)
			}
		}
	}

	return nil
}

// Store builds all buffered per-day TSDB indexes, uploads them to the bucket,
// and resets the internal state.
func (b *dataobjTSDBBuilder) Store(ctx context.Context) error {
	days := make([]time.Time, 0, len(b.dayBuilders))
	for d := range b.dayBuilders {
		days = append(days, d)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })

	for _, day := range days {
		builder := b.dayBuilders[day]

		tsdbId, tsdbData, err := builder.BuildInMemory(ctx, func(from, through model.Time, checksum uint32) index_tsdb.Identifier {
			return dailyTSDBIdentifier{
				inner: index_tsdb.MultitenantTSDBIdentifier{
					NodeName: b.nodeName,
					Ts:       time.Now().UTC(),
				},
				day: day,
			}
		})
		if err != nil {
			return fmt.Errorf("building TSDB for %s: %w", day.Format("2006-01-02"), err)
		}

		sectionRefData, err := builder.SectionRefTable().Encode()
		if err != nil {
			return fmt.Errorf("encoding section ref table for %s: %w", day.Format("2006-01-02"), err)
		}

		if err := store(ctx, b.bkt, tsdbId, tsdbData, sectionRefData); err != nil {
			return err
		}
	}

	b.dayBuilders = make(map[time.Time]*index_tsdb.Builder)
	return nil
}

// dailyTSDBIdentifier wraps an identifier to prefix its path with the day
// directory: <day>/
type dailyTSDBIdentifier struct {
	inner index_tsdb.Identifier
	day   time.Time
}

func (d dailyTSDBIdentifier) Path() string {
	return fmt.Sprintf("index_%d/%s", d.day.Unix()/int64(dayWindow.Seconds()), d.inner.Path())
}

func (d dailyTSDBIdentifier) Name() string {
	return d.inner.Name()
}

func collectStreamLabels(ctx context.Context, obj *dataobj.Object) (map[streamKey]labels.Labels, error) {
	res := map[streamKey]labels.Labels{}

	for _, sec := range obj.Sections() {
		if !streams.CheckSection(sec) {
			continue
		}

		streamsSection, err := streams.Open(ctx, sec)
		if err != nil {
			return nil, fmt.Errorf("opening streams section: %w", err)
		}

		for rec := range streams.IterSection(ctx, streamsSection, streams.WithReuseLabelsBuffer()) {
			if err := rec.Err(); err != nil {
				return nil, fmt.Errorf("iterating streams section: %w", err)
			}
			s := rec.MustValue()
			key := streamKey{tenant: sec.Tenant, streamID: s.ID}

			labelsBuilder := labels.NewScratchBuilder(s.Labels.Len())
			s.Labels.Range(func(l labels.Label) {
				labelsBuilder.Add(l.Name, l.Value)
			})
			labelsBuilder.Sort()
			labelsBuilder.Add(index_tsdb.TenantLabel, sec.Tenant)
			res[key] = labelsBuilder.Labels()
		}
	}

	return res, nil
}

func collectSectionMetas(ctx context.Context, obj *dataobj.Object, objectPath string) (map[streamKey][]sectionref.SectionMeta, error) {
	perStream := map[streamKey][]sectionref.SectionMeta{}

	var logsSectionIdx int
	for _, sec := range obj.Sections() {
		if !logs.CheckSection(sec) {
			continue
		}

		logsSection, err := logs.Open(ctx, sec)
		if err != nil {
			return nil, fmt.Errorf("opening logs section: %w", err)
		}

		statsByStream := map[streamKey]*sectionStats{}

		for rec := range logs.IterSection(ctx, logsSection) {
			if err := rec.Err(); err != nil {
				return nil, fmt.Errorf("iterating logs section: %w", err)
			}
			r := rec.MustValue()
			key := streamKey{tenant: sec.Tenant, streamID: r.StreamID}
			stats, ok := statsByStream[key]
			if !ok {
				stats = &sectionStats{}
				statsByStream[key] = stats
			}

			ts := r.Timestamp.UnixMilli()
			if !stats.set || ts < stats.minTime {
				stats.minTime = ts
			}
			if !stats.set || ts > stats.maxTime {
				stats.maxTime = ts
			}
			stats.set = true
			stats.entries++

			stats.bytes += int64(len(r.Line))
			r.Metadata.Range(func(l labels.Label) {
				stats.bytes += int64(len(l.Value))
			})
		}

		for key, stats := range statsByStream {
			perStream[key] = append(perStream[key], sectionref.SectionMeta{
				SectionRef: sectionref.SectionRef{
					Path:      objectPath,
					SectionID: logsSectionIdx,
					SeriesID:  int(key.streamID),
				},
				ChunkMeta: tsdbindex.ChunkMeta{
					MinTime: stats.minTime,
					MaxTime: stats.maxTime,
					KB:      uint32((stats.bytes + 1023) / 1024),
					Entries: stats.entries,
				},
			})
		}
		logsSectionIdx++
	}

	return perStream, nil
}

func store(ctx context.Context, bkt objstore.Bucket, id index_tsdb.Identifier, tsdbData []byte, sectionRefData []byte) error {
	sectionRefBuffer := bytes.NewBuffer(nil)
	tsdbBuffer := bytes.NewBuffer(nil)
	sectionRefWriter := gzip.NewWriter(sectionRefBuffer)
	tsdbWriter := gzip.NewWriter(tsdbBuffer)

	_, err := sectionRefWriter.Write(sectionRefData)
	if err != nil {
		return err
	}
	sectionRefWriter.Close()

	_, err = tsdbWriter.Write(tsdbData)
	if err != nil {
		return err
	}
	tsdbWriter.Close()

	if err := bkt.Upload(ctx, id.Path()+".sections.gz", sectionRefBuffer); err != nil {
		return err
	}
	if err := bkt.Upload(ctx, id.Path()+".gz", tsdbBuffer); err != nil {
		return err
	}

	return nil
}

var _ tsdbBuilder = (*dataobjTSDBBuilder)(nil)
