package consumer

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
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
	BuildAndStore(ctx context.Context, obj *dataobj.Object, objectPath string) error
}

func newTSDBBuilder(nodeName string, bkt objstore.Bucket) tsdbBuilder {
	return &dataobjTSDBBuilder{nodeName: nodeName, bkt: bkt}
}

type dataobjTSDBBuilder struct {
	nodeName string
	bkt      objstore.Bucket
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

func (b *dataobjTSDBBuilder) BuildAndStore(ctx context.Context, obj *dataobj.Object, objectPath string) error {
	id, tsdbData, sectionRefData, err := b.build(ctx, obj, objectPath)
	if err != nil {
		return err
	}

	if err := store(ctx, b.bkt, id, tsdbData, sectionRefData); err != nil {
		return err
	}

	return nil
}

func (b *dataobjTSDBBuilder) build(ctx context.Context, obj *dataobj.Object, objectPath string) (index_tsdb.Identifier, []byte, []byte, error) {
	streamLabels, err := collectStreamLabels(ctx, obj)
	if err != nil {
		return nil, nil, nil, err
	}

	streamSectionMetas, err := collectSectionMetas(ctx, obj, objectPath)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(streamSectionMetas) == 0 {
		return nil, nil, nil, nil
	}

	tsdbBuilder := index_tsdb.NewBuilder(tsdbindex.FormatV3)
	for key, metas := range streamSectionMetas {
		lbls, ok := streamLabels[key]
		if !ok {
			return nil, nil, nil, fmt.Errorf("missing stream labels for tenant=%q streamID=%d", key.tenant, key.streamID)
		}

		fp := model.Fingerprint(labels.StableHash(lbls))
		if err := tsdbBuilder.AddSeriesWithSectionRefs(lbls, fp, metas); err != nil {
			return nil, nil, nil, fmt.Errorf("adding stream to tsdb builder: %w", err)
		}
	}

	tsdbId, tsdbData, err := tsdbBuilder.BuildInMemory(ctx, func(from, through model.Time, checksum uint32) index_tsdb.Identifier {
		return index_tsdb.MultitenantTSDBIdentifier{
			NodeName: "dataobj-consumer",
			Ts:       time.Now(),
		}
	})
	sectionRefData, err := tsdbBuilder.SectionRefTable().Encode()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("encoding section ref table: %w", err)
	}
	return tsdbId, tsdbData, sectionRefData, nil
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

	for sectionID, sec := range obj.Sections() {
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
					SectionID: sectionID,
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
