package bloomshipper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/concurrency"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/util/encoding"
)

const (
	rootFolder            = "bloom"
	metasFolder           = "metas"
	bloomsFolder          = "blooms"
	delimiter             = "/"
	fileNamePartDelimiter = "-"
)

type Ref struct {
	TenantID                     string
	TableName                    string
	Bounds                       v1.FingerprintBounds
	StartTimestamp, EndTimestamp model.Time
	Checksum                     uint32
}

// Hash hashes the ref
// NB(owen-d): we don't include the tenant in the hash
// as it's not included in the data and leaving it out gives
// flexibility for migrating data between tenants
func (r Ref) Hash(h hash.Hash32) error {
	if err := r.Bounds.Hash(h); err != nil {
		return err
	}

	var enc encoding.Encbuf

	enc.PutString(r.TableName)
	enc.PutBE64(uint64(r.StartTimestamp))
	enc.PutBE64(uint64(r.EndTimestamp))
	enc.PutBE32(r.Checksum)

	_, err := h.Write(enc.Get())
	return errors.Wrap(err, "writing BlockRef")
}

// Cmp returns the fingerprint's position relative to the bounds
func (r Ref) Cmp(fp uint64) v1.BoundsCheck {
	return r.Bounds.Cmp(model.Fingerprint(fp))
}

func (r Ref) Interval() Interval {
	return NewInterval(r.StartTimestamp, r.EndTimestamp)
}

type BlockRef struct {
	Ref
}

func (r BlockRef) String() string {
	return defaultKeyResolver{}.Block(r).Addr()
}

type MetaRef struct {
	Ref
	FilePath string
}

// todo rename it
type Meta struct {
	MetaRef `json:"-"`

	Tombstones []BlockRef
	Blocks     []BlockRef
}

type MetaSearchParams struct {
	TenantID string
	Interval Interval
	Keyspace v1.FingerprintBounds
}

type MetaClient interface {
	// Returns all metas that are within MinFingerprint-MaxFingerprint fingerprint range
	// and intersect time period from StartTimestamp to EndTimestamp.
	GetMetas(ctx context.Context, metas []MetaRef) ([]Meta, error)
	PutMeta(ctx context.Context, meta Meta) error
	DeleteMeta(ctx context.Context, meta Meta) error
}

type LazyBlock struct {
	BlockRef
	Data io.ReadCloser
}

type Block struct {
	BlockRef
	Data io.ReadSeekCloser
}

type BlockClient interface {
	KeyResolver
	GetBlock(ctx context.Context, ref BlockRef) (LazyBlock, error)
	PutBlocks(ctx context.Context, blocks []Block) ([]Block, error)
	DeleteBlocks(ctx context.Context, blocks []BlockRef) error
}

type Client interface {
	MetaClient
	BlockClient
	Stop()
}

// Compiler check to ensure BloomClient implements the Client interface
var _ Client = &BloomClient{}

type BloomClient struct {
	KeyResolver
	concurrency int
	client      client.ObjectClient
	logger      log.Logger
}

func NewBloomClient(client client.ObjectClient, logger log.Logger) (*BloomClient, error) {
	return &BloomClient{
		KeyResolver: defaultKeyResolver{}, // TODO(owen-d): hook into schema, similar to `{,Parse}ExternalKey`
		concurrency: 100,                  // make configurable?
		client:      client,
		logger:      logger,
	}, nil
}

func (b *BloomClient) PutMeta(ctx context.Context, meta Meta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("can not marshal the meta to json: %w", err)
	}
	key := externalMetaKey(meta.MetaRef)
	return b.client.PutObject(ctx, key, bytes.NewReader(data))
}

func externalMetaKey(ref MetaRef) string {
	filename := fmt.Sprintf("%s-%d-%d-%x", ref.Bounds.String(), ref.StartTimestamp, ref.EndTimestamp, ref.Checksum)
	return path.Join(rootFolder, ref.TableName, ref.TenantID, metasFolder, filename)
}

func findPeriod(configs []config.PeriodConfig, ts model.Time) (config.DayTime, error) {
	for i := len(configs) - 1; i >= 0; i-- {
		periodConfig := configs[i]
		if periodConfig.From.Before(ts) || periodConfig.From.Equal(ts) {
			return periodConfig.From, nil
		}
	}
	return config.DayTime{}, fmt.Errorf("can not find period for timestamp %d", ts)
}

func (b *BloomClient) DeleteMeta(ctx context.Context, meta Meta) error {
	key := externalMetaKey(meta.MetaRef)
	return b.client.DeleteObject(ctx, key)
}

// GetBlock downloads the blocks from objectStorage and returns the downloaded block
func (b *BloomClient) GetBlock(ctx context.Context, reference BlockRef) (LazyBlock, error) {
	readCloser, _, err := b.client.GetObject(ctx, b.Block(reference).Addr())
	if err != nil {
		return LazyBlock{}, fmt.Errorf("error while fetching object from storage: %w", err)
	}
	return LazyBlock{
		BlockRef: reference,
		Data:     readCloser,
	}, nil
}

func (b *BloomClient) PutBlocks(ctx context.Context, blocks []Block) ([]Block, error) {
	results := make([]Block, len(blocks))
	err := concurrency.ForEachJob(ctx, len(blocks), b.concurrency, func(ctx context.Context, idx int) error {
		block := blocks[idx]
		defer func(Data io.ReadCloser) {
			_ = Data.Close()
		}(block.Data)

		var err error

		key := b.Block(block.BlockRef).Addr()
		_, err = block.Data.Seek(0, 0)
		if err != nil {
			return fmt.Errorf("error uploading block file: %w", err)
		}

		err = b.client.PutObject(ctx, key, block.Data)
		if err != nil {
			return fmt.Errorf("error uploading block file: %w", err)
		}
		results[idx] = block
		return nil
	})
	return results, err
}

func (b *BloomClient) DeleteBlocks(ctx context.Context, references []BlockRef) error {
	return concurrency.ForEachJob(ctx, len(references), b.concurrency, func(ctx context.Context, idx int) error {
		ref := references[idx]
		key := b.Block(ref).Addr()
		err := b.client.DeleteObject(ctx, key)
		if err != nil {
			return fmt.Errorf("error deleting block file: %w", err)
		}
		return nil
	})
}

func (b *BloomClient) Stop() {
	b.client.Stop()
}

func (b *BloomClient) GetMetas(ctx context.Context, refs []MetaRef) ([]Meta, error) {
	results := make([]Meta, len(refs))
	err := concurrency.ForEachJob(ctx, len(refs), b.concurrency, func(ctx context.Context, idx int) error {
		meta, err := b.getMeta(ctx, refs[idx])
		if err != nil {
			return err
		}
		results[idx] = meta
		return nil
	})
	return results, err
}

func (b *BloomClient) getMeta(ctx context.Context, ref MetaRef) (Meta, error) {
	meta := Meta{
		MetaRef: ref,
	}
	reader, _, err := b.client.GetObject(ctx, ref.FilePath)
	if err != nil {
		return Meta{}, fmt.Errorf("error downloading meta file %s : %w", ref.FilePath, err)
	}
	defer reader.Close()

	err = json.NewDecoder(reader).Decode(&meta)
	if err != nil {
		return Meta{}, fmt.Errorf("error unmarshalling content of meta file %s: %w", ref.FilePath, err)
	}
	return meta, nil
}

func createMetaRef(objectKey string, tenantID string, tableName string) (MetaRef, error) {
	fileName := objectKey[strings.LastIndex(objectKey, delimiter)+1:]
	parts := strings.Split(fileName, fileNamePartDelimiter)
	if len(parts) != 5 {
		return MetaRef{}, fmt.Errorf("%s filename parts count must be 5 but was %d: [%s]", objectKey, len(parts), strings.Join(parts, ", "))
	}
	bounds, err := v1.ParseBoundsFromParts(parts[0], parts[1])
	if err != nil {
		return MetaRef{}, fmt.Errorf("error parsing bounds %s : %w", parts[0], err)
	}

	startTimestamp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return MetaRef{}, fmt.Errorf("error parsing startTimestamp %s : %w", parts[2], err)
	}
	endTimestamp, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return MetaRef{}, fmt.Errorf("error parsing endTimestamp %s : %w", parts[3], err)
	}
	checksum, err := strconv.ParseUint(parts[4], 16, 64)
	if err != nil {
		return MetaRef{}, fmt.Errorf("error parsing checksum %s : %w", parts[4], err)
	}
	return MetaRef{
		Ref: Ref{
			TenantID:       tenantID,
			TableName:      tableName,
			Bounds:         bounds,
			StartTimestamp: model.Time(startTimestamp),
			EndTimestamp:   model.Time(endTimestamp),
			Checksum:       uint32(checksum),
		},
		FilePath: objectKey,
	}, nil
}

func tablesForRange(periodConfig config.PeriodConfig, interval Interval) []string {
	step := int64(periodConfig.IndexTables.Period.Seconds())
	lower := interval.Start.Unix() / step
	upper := interval.End.Unix() / step
	tables := make([]string, 0, 1+upper-lower)
	for i := lower; i <= upper; i++ {
		tables = append(tables, fmt.Sprintf("%s%d", periodConfig.IndexTables.Prefix, i))
	}
	return tables
}
