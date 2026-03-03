package tsdb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	shipperindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/sectionref"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var ErrAlreadyOnDesiredVersion = errors.New("tsdb file already on desired version")

// GetRawFileReaderFunc returns an io.ReadSeeker for reading raw tsdb file from disk
type GetRawFileReaderFunc func() (io.ReadSeeker, error)

func OpenShippableTSDB(p string) (shipperindex.Index, error) {
	id, err := identifierFromPath(p)
	if err != nil {
		return nil, err
	}

	return NewShippableTSDBFile(id)
}

func RebuildWithVersion(ctx context.Context, path string, desiredVer int) (shipperindex.Index, error) {
	indexFile, err := OpenShippableTSDB(path)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err := indexFile.Close(); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to close index file", "err", err)
		}
	}()

	currVer := indexFile.(*TSDBFile).Index.(*TSDBIndex).reader.(*index.Reader).Version()
	if currVer == desiredVer {
		return nil, ErrAlreadyOnDesiredVersion
	}

	builder := NewBuilder(desiredVer)
	err = indexFile.(*TSDBFile).Index.(*TSDBIndex).ForSeries(ctx, "", nil, 0, math.MaxInt64, func(lbls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) (stop bool) {
		builder.AddSeries(lbls.Copy(), fp, chks)
		return false
	}, labels.MustNewMatcher(labels.MatchEqual, "", ""))
	if err != nil {
		return nil, err
	}

	parentDir := filepath.Dir(path)

	id, err := builder.Build(ctx, parentDir, func(from, through model.Time, checksum uint32) Identifier {
		id := SingleTenantTSDBIdentifier{
			TS:       time.Now(),
			From:     from,
			Through:  through,
			Checksum: checksum,
		}
		return NewPrefixedIdentifier(id, parentDir, "")
	})

	if err != nil {
		return nil, err
	}
	return NewShippableTSDBFile(id)
}

// nolint
// TSDBFile is backed by an actual file and implements the indexshipper/index.Index interface
type TSDBFile struct {
	// reuse Identifier for resolving locations
	Identifier

	// reuse TSDBIndex for reading
	Index

	// to sastisfy Reader() and Close() methods
	getRawFileReader GetRawFileReaderFunc
}

func NewShippableTSDBFile(id Identifier) (*TSDBFile, error) {
	idx, getRawFileReader, err := NewTSDBIndexFromFile(id.Path())
	if err != nil {
		return nil, err
	}

	lookupPath := strings.TrimSuffix(id.Path(), ".tsdb") + ".lookup"
	if data, readErr := os.ReadFile(lookupPath); readErr == nil {
		table, decErr := sectionref.Decode(data)
		if decErr != nil {
			return nil, fmt.Errorf("decoding lookup table %s: %w", lookupPath, decErr)
		}
		idx.sectionRefTable = table
	}

	return &TSDBFile{
		Identifier:       id,
		Index:            idx,
		getRawFileReader: getRawFileReader,
	}, nil
}

func (f *TSDBFile) Close() error {
	return f.Index.Close()
}

func (f *TSDBFile) Reader() (io.ReadSeeker, error) {
	return f.getRawFileReader()
}

// nolint
// TSDBIndex is backed by an IndexReader
// and translates the IndexReader to an Index implementation
// It loads the file into memory and doesn't keep a file descriptor open
type TSDBIndex struct {
	reader          IndexReader
	chunkFilter     chunk.RequestChunkFilterer
	sectionRefTable *sectionref.SectionRefTable
}

// Return the index as well as the underlying raw file reader which isn't exposed as an index
// method but is helpful for building an io.reader for the index shipper
func NewTSDBIndexFromFile(location string) (*TSDBIndex, GetRawFileReaderFunc, error) {
	reader, err := index.NewFileReader(location)
	if err != nil {
		return nil, nil, err
	}

	return NewTSDBIndex(reader), func() (io.ReadSeeker, error) {
		return reader.RawFileReader()
	}, nil
}

func NewTSDBIndex(reader IndexReader) *TSDBIndex {
	return &TSDBIndex{
		reader: reader,
	}
}

func (i *TSDBIndex) Close() error {
	return i.reader.Close()
}

func (i *TSDBIndex) Bounds() (model.Time, model.Time) {
	from, through := i.reader.Bounds()
	return model.Time(from), model.Time(through)
}

func (i *TSDBIndex) SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer) {
	i.chunkFilter = chunkFilter
}

// fn must NOT capture it's arguments. They're reused across series iterations and returned to
// a pool after completion.
// Iteration will stop if the callback returns true.
// Accepts a userID argument in order to implement `Index` interface, but since this is a single tenant index,
// it is ignored (it's enforced elsewhere in index selection)
func (i *TSDBIndex) ForSeries(ctx context.Context, _ string, fpFilter index.FingerprintFilter, from model.Time, through model.Time, fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta) (stop bool), matchers ...*labels.Matcher) error {
	var filterer chunk.Filterer
	if i.chunkFilter != nil {
		filterer = i.chunkFilter.ForRequest(ctx)
	}
	return i.forSeriesAndLabels(ctx, fpFilter, filterer, from, through, fn, matchers...)
}

func (i *TSDBIndex) forSeriesAndLabels(ctx context.Context, fpFilter index.FingerprintFilter, filterer chunk.Filterer, from model.Time, through model.Time, fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta) (stop bool), matchers ...*labels.Matcher) error {
	// TODO(owen-d): use pool

	var ls labels.Labels
	chks := ChunkMetasPool.Get()
	defer func() { ChunkMetasPool.Put(chks) }()

	return i.forPostings(ctx, fpFilter, from, through, matchers, func(p index.Postings) error {
		for p.Next() {
			hash, err := i.reader.Series(p.At(), int64(from), int64(through), &ls, &chks)
			if err != nil {
				return err
			}

			// skip series that belong to different shards
			if fpFilter != nil && !fpFilter.Match(model.Fingerprint(hash)) {
				continue
			}

			if filterer != nil && filterer.ShouldFilter(ls) {
				continue
			}

			if stop := fn(ls, model.Fingerprint(hash), chks); stop {
				break
			}
		}
		return p.Err()
	})

}

// Same as ForSeries, but the callback fn does not take a Labels parameter.
func (i *TSDBIndex) forSeriesNoLabels(ctx context.Context, fpFilter index.FingerprintFilter, from model.Time, through model.Time, fn func(model.Fingerprint, []index.ChunkMeta) (stop bool), matchers ...*labels.Matcher) error {
	chks := ChunkMetasPool.Get()
	defer ChunkMetasPool.Put(chks)

	return i.forPostings(ctx, fpFilter, from, through, matchers, func(p index.Postings) error {
		for p.Next() {
			hash, err := i.reader.Series(p.At(), int64(from), int64(through), nil, &chks)
			if err != nil {
				return err
			}

			// skip series that belong to different shards
			if fpFilter != nil && !fpFilter.Match(model.Fingerprint(hash)) {
				continue
			}

			if stop := fn(model.Fingerprint(hash), chks); stop {
				break
			}
		}
		return p.Err()
	})
}

func (i *TSDBIndex) forPostings(
	_ context.Context,
	fpFilter index.FingerprintFilter,
	_, _ model.Time,
	matchers []*labels.Matcher,
	fn func(index.Postings) error,
) error {
	p, err := PostingsForMatchers(i.reader, fpFilter, matchers...)
	if err != nil {
		return err
	}
	return fn(p)
}

func (i *TSDBIndex) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, res []logproto.ChunkRefWithSizingInfo, fpFilter index.FingerprintFilter, matchers ...*labels.Matcher) ([]logproto.ChunkRefWithSizingInfo, error) {
	if res == nil {
		res = ChunkRefsPool.Get()
	}
	res = res[:0]

	addChunksToResult := func(fp model.Fingerprint, chks []index.ChunkMeta) (stop bool) {
		for _, chk := range chks {
			res = append(res, logproto.ChunkRefWithSizingInfo{
				ChunkRef: logproto.ChunkRef{
					UserID:      userID, // assumed to be the same, will be enforced by caller.
					Fingerprint: uint64(fp),
					From:        chk.From(),
					Through:     chk.Through(),
					Checksum:    chk.Checksum,
				},
				KB:      chk.KB,
				Entries: chk.Entries,
			})
		}
		return false
	}

	var filterer chunk.Filterer
	if i.chunkFilter != nil {
		filterer = i.chunkFilter.ForRequest(ctx)
	}
	var err error
	if filterer != nil {
		// We need to fetch labels to pass to the filterer, even though we don't look at them in the callback.
		err = i.forSeriesAndLabels(ctx, fpFilter, filterer, from, through, func(_ labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) (stop bool) {
			return addChunksToResult(fp, chks)
		}, matchers...)
	} else {
		err = i.forSeriesNoLabels(ctx, fpFilter, from, through, addChunksToResult, matchers...)
	}
	return res, err
}

func (i *TSDBIndex) Series(ctx context.Context, _ string, from, through model.Time, res []Series, fpFilter index.FingerprintFilter, matchers ...*labels.Matcher) ([]Series, error) {
	if res == nil {
		res = SeriesPool.Get()
	}
	res = res[:0]

	if err := i.ForSeries(ctx, "", fpFilter, from, through, func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) (stop bool) {
		if len(chks) == 0 {
			return
		}
		res = append(res, Series{
			Labels:      ls.Copy(),
			Fingerprint: fp,
		})
		return false
	}, matchers...); err != nil {
		return nil, err
	}

	return res, nil
}

func (i *TSDBIndex) LabelNames(_ context.Context, _ string, _, _ model.Time, matchers ...*labels.Matcher) ([]string, error) {
	if len(matchers) == 0 {
		return i.reader.LabelNames()
	}

	return labelNamesWithMatchers(i.reader, matchers...)
}

func (i *TSDBIndex) LabelValues(_ context.Context, _ string, _, _ model.Time, name string, matchers ...*labels.Matcher) ([]string, error) {
	if len(matchers) != 0 {
		return labelValuesWithMatchers(i.reader, name, matchers...)
	}

	labelValues, err := i.reader.LabelValues(name)
	if err != nil {
		return nil, err
	}

	// cloning the string
	return cloneStringList(labelValues), nil
}

func (i *TSDBIndex) Checksum() uint32 {
	return i.reader.Checksum()
}

func (i *TSDBIndex) Identifier(string) SingleTenantTSDBIdentifier {
	lower, upper := i.Bounds()
	return SingleTenantTSDBIdentifier{
		TS:       time.Now(),
		From:     lower,
		Through:  upper,
		Checksum: i.Checksum(),
	}
}

func (i *TSDBIndex) Stats(ctx context.Context, _ string, from, through model.Time, acc IndexStatsAccumulator, fpFilter index.FingerprintFilter, _ shouldIncludeChunk, matchers ...*labels.Matcher) error {
	return i.forPostings(ctx, fpFilter, from, through, matchers, func(p index.Postings) error {
		// TODO(owen-d): use pool
		var ls labels.Labels
		var filterer chunk.Filterer
		by := make(map[string]struct{})
		if i.chunkFilter != nil {
			filterer = i.chunkFilter.ForRequest(ctx)
			if filterer != nil {
				for _, k := range filterer.RequiredLabelNames() {
					by[k] = struct{}{}
				}
			}
		}

		for p.Next() {
			fp, stats, err := i.reader.ChunkStats(p.At(), int64(from), int64(through), &ls, by)
			if err != nil {
				return err
			}

			// skip series that belong to different shards
			if fpFilter != nil && !fpFilter.Match(model.Fingerprint(fp)) {
				continue
			}

			if filterer != nil && filterer.ShouldFilter(ls) {
				continue
			}

			if stats.Entries > 0 {
				// need to add stream
				acc.AddStream(model.Fingerprint(fp))
				acc.AddChunkStats(stats)
			}
		}
		return p.Err()
	})
}

// Volume returns the volumes of the series described by the passed
// matchers by the names of the passed matchers. All non-requested labels are
// aggregated into the requested series.
//
// ex: Imagine we have two labels: 'foo' and 'fizz' each with two values 'a'
// and 'b'. Called with the matcher `{foo="a"}`, Volume returns the
// aggregated size of the series `{foo="a"}`. If Volume with
// `{foo=~".+", fizz=~".+"}, it returns the volumes aggregated as follows:
//
// {foo="a", fizz="a"}
// {foo="a", fizz="b"}
// {foo="b", fizz="a"}
// {foo="b", fizz="b"}
//
// Volume optionally accepts a slice of target labels. If provided, volumes are aggregated
// into those labels only. For example, given the matcher {fizz=~".+"} and target labels of []string{"foo"},
// volumes would be aggregated as follows:
//
// {foo="a"} which would be the sum of {foo="a", fizz="a"} and {foo="a", fizz="b"}
// {foo="b"} which would be the sum of {foo="b", fizz="a"} and {foo="b", fizz="b"}
func (i *TSDBIndex) Volume(
	ctx context.Context,
	_ string,
	from, through model.Time,
	acc VolumeAccumulator,
	fpFilter index.FingerprintFilter,
	_ shouldIncludeChunk,
	targetLabels []string,
	aggregateBy string,
	matchers ...*labels.Matcher,
) error {
	ctx, sp := tracer.Start(ctx, "Index.Volume")
	defer sp.End()

	labelsToMatch, matchers, includeAll := util.PrepareLabelsAndMatchers(targetLabels, matchers, TenantLabel)

	seriesNames := make(map[uint64]string)
	seriesLabelsBuilder := labels.NewScratchBuilder(len(labelsToMatch))

	aggregateBySeries := seriesvolume.AggregateBySeries(aggregateBy) || aggregateBy == ""
	var by map[string]struct{}
	var filterer chunk.Filterer
	if i.chunkFilter != nil {
		filterer = i.chunkFilter.ForRequest(ctx)
	}
	if !includeAll && (aggregateBySeries || len(targetLabels) > 0) {
		by = make(map[string]struct{}, len(labelsToMatch))
		for k := range labelsToMatch {
			by[k] = struct{}{}
		}

		// If we are aggregating by series, we need to include all labels in the series required for filtering chunks.
		if filterer != nil {
			for _, k := range filterer.RequiredLabelNames() {
				by[k] = struct{}{}
			}
		}
	}

	return i.forPostings(ctx, fpFilter, from, through, matchers, func(p index.Postings) error {
		var ls labels.Labels
		for p.Next() {
			fp, stats, err := i.reader.ChunkStats(p.At(), int64(from), int64(through), &ls, by)
			if err != nil {
				return fmt.Errorf("series volume: %w", err)
			}

			// skip series that belong to different shards
			if fpFilter != nil && !fpFilter.Match(model.Fingerprint(fp)) {
				continue
			}

			if filterer != nil && filterer.ShouldFilter(ls) {
				continue
			}

			if stats.Entries > 0 {
				var labelVolumes map[string]uint64

				if aggregateBySeries {
					seriesLabelsBuilder.Reset()
					ls.Range(func(l labels.Label) {
						if _, ok := labelsToMatch[l.Name]; l.Name != TenantLabel && includeAll || ok {
							seriesLabelsBuilder.Add(l.Name, l.Value)
						}
					})
				} else {
					// when aggregating by labels, capture sizes for target labels if provided,
					// otherwise for all intersecting labels
					labelVolumes = make(map[string]uint64, ls.Len())
					ls.Range(func(l labels.Label) {
						if len(targetLabels) > 0 {
							if _, ok := labelsToMatch[l.Name]; l.Name != TenantLabel && includeAll || ok {
								labelVolumes[l.Name] += stats.KB << 10
							}
						} else {
							if l.Name != TenantLabel {
								labelVolumes[l.Name] += stats.KB << 10
							}
						}
					})
				}

				seriesLabelsBuilder.Sort()
				seriesLabels := seriesLabelsBuilder.Labels()

				// If the labels are < 1k, this does not alloc
				// https://github.com/prometheus/prometheus/pull/8025
				hash := labels.StableHash(seriesLabels)
				if _, ok := seriesNames[hash]; !ok {
					seriesNames[hash] = seriesLabels.String()
				}

				if aggregateBySeries {
					if err = acc.AddVolume(seriesNames[hash], stats.KB<<10); err != nil {
						return err
					}
				} else {
					for label, volume := range labelVolumes {
						if err = acc.AddVolume(label, volume); err != nil {
							return err
						}
					}
				}
			}
		}
		return p.Err()
	})
}

// GetDataobjSections resolves matching dataobj references using the companion
// lookup table and returns fully resolved dataobj section references grouped
// by (Path, SectionID). SeriesIDs are read from the sidecar SectionRefTable
// and collected per section.
func (i *TSDBIndex) GetDataobjSections(
	ctx context.Context,
	userID string,
	from, through model.Time,
	fpFilter index.FingerprintFilter,
	matchers ...*labels.Matcher,
) ([]DataobjSectionRef, error) {
	if i.sectionRefTable == nil {
		return nil, fmt.Errorf("no section ref lookup table loaded for this index")
	}

	// key to uniquely identify a dataobj section.
	type sectionKey struct {
		Path      string
		SectionID int
	}

	type accumulator struct {
		ref       DataobjSectionRef
		streamSet map[int64]struct{}
	}

	sections := make(map[sectionKey]*accumulator)

	var failedLookup bool

	if err := i.ForSeries(ctx, userID, fpFilter, from, through, func(_ labels.Labels, _ model.Fingerprint, chks []index.ChunkMeta) (stop bool) {
		for _, chk := range chks {
			ref, ok := i.sectionRefTable.Lookup(chk.Checksum)
			if !ok {
				failedLookup = true
				return true // stop iterator.
			}

			k := sectionKey{Path: ref.Path, SectionID: ref.SectionID}

			acc, ok := sections[k]
			if !ok {
				acc = &accumulator{
					ref: DataobjSectionRef{
						Path:      ref.Path,
						SectionID: ref.SectionID,
						MinTime:   chk.From(),
						MaxTime:   chk.Through(),
						KB:        chk.KB,
						Entries:   chk.Entries,
					},
					streamSet: make(map[int64]struct{}),
				}
				sections[k] = acc
			} else {
				if chk.From() < acc.ref.MinTime {
					acc.ref.MinTime = chk.From()
				}
				if chk.Through() > acc.ref.MaxTime {
					acc.ref.MaxTime = chk.Through()
				}

				// do not add these up, as these reflect entire section stats.
				// acc.ref.KB += chk.KB
				// acc.ref.Entries += chk.Entries
			}
			acc.streamSet[int64(ref.SeriesID)] = struct{}{}
		}
		return false
	}, matchers...); err != nil {
		return nil, err
	}

	if failedLookup {
		return nil, fmt.Errorf("failed to lookup section ref for at least one chunk; cannot resolve dataobj sections")
	}

	res := make([]DataobjSectionRef, 0, len(sections))
	for _, acc := range sections {
		ids := make([]int64, 0, len(acc.streamSet))
		for id := range acc.streamSet {
			ids = append(ids, id)
		}
		acc.ref.StreamIDs = ids
		res = append(res, acc.ref)
	}
	return res, nil
}

func cloneStringList(strs []string) []string {
	res := make([]string, 0, len(strs))
	for _, str := range strs {
		res = append(res, strings.Clone(str))
	}
	return res
}
