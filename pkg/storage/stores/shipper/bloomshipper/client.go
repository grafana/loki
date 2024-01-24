package bloomshipper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/dskit/concurrency"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/storage"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/util/math"
)

const (
	rootFolder            = "bloom"
	metasFolder           = "metas"
	bloomsFolder          = "blooms"
	delimiter             = "/"
	fileNamePartDelimiter = "-"
)

type Ref struct {
	TenantID                       string
	TableName                      string
	MinFingerprint, MaxFingerprint uint64
	StartTimestamp, EndTimestamp   model.Time
	Checksum                       uint32
}

// Cmp returns the fingerprint's position relative to the bounds
func (r Ref) Cmp(fp uint64) v1.BoundsCheck {
	if fp < r.MinFingerprint {
		return v1.Before
	} else if fp > r.MaxFingerprint {
		return v1.After
	}
	return v1.Overlap
}

type BlockRef struct {
	Ref
	IndexPath string
	BlockPath string
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
	TenantID                       string
	MinFingerprint, MaxFingerprint model.Fingerprint
	StartTimestamp, EndTimestamp   model.Time
}

type MetaClient interface {
	// Returns all metas that are within MinFingerprint-MaxFingerprint fingerprint range
	// and intersect time period from StartTimestamp to EndTimestamp.
	GetMetas(ctx context.Context, params MetaSearchParams) ([]Meta, error)
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
	GetBlock(ctx context.Context, reference BlockRef) (LazyBlock, error)
	PutBlocks(ctx context.Context, blocks []Block) ([]Block, error)
	DeleteBlocks(ctx context.Context, blocks []BlockRef) error
}

type Client interface {
	MetaClient
	BlockClient
	Stop()
}

// todo add logger
func NewBloomClient(periodicConfigs []config.PeriodConfig, storageConfig storage.Config, clientMetrics storage.ClientMetrics) (*BloomClient, error) {
	periodicObjectClients := make(map[config.DayTime]client.ObjectClient)
	for _, periodicConfig := range periodicConfigs {
		objectClient, err := storage.NewObjectClient(periodicConfig.ObjectType, storageConfig, clientMetrics)
		if err != nil {
			return nil, fmt.Errorf("error creating object client '%s': %w", periodicConfig.ObjectType, err)
		}
		periodicObjectClients[periodicConfig.From] = objectClient
	}
	return &BloomClient{
		periodicConfigs:       periodicConfigs,
		storageConfig:         storageConfig,
		periodicObjectClients: periodicObjectClients,
	}, nil
}

type BloomClient struct {
	periodicConfigs       []config.PeriodConfig
	storageConfig         storage.Config
	periodicObjectClients map[config.DayTime]client.ObjectClient
}

func (b *BloomClient) GetMetas(ctx context.Context, params MetaSearchParams) ([]Meta, error) {
	tablesByPeriod := tablesByPeriod(b.periodicConfigs, params.StartTimestamp, params.EndTimestamp)

	var metas []Meta
	for periodFrom, tables := range tablesByPeriod {
		periodClient := b.periodicObjectClients[periodFrom]
		for _, table := range tables {
			prefix := filepath.Join(rootFolder, table, params.TenantID, metasFolder)
			list, _, err := periodClient.List(ctx, prefix, "")
			if err != nil {
				return nil, fmt.Errorf("error listing metas under prefix [%s]: %w", prefix, err)
			}
			for _, object := range list {
				metaRef, err := createMetaRef(object.Key, params.TenantID, table)

				if err != nil {
					return nil, err
				}
				if metaRef.MaxFingerprint < uint64(params.MinFingerprint) || uint64(params.MaxFingerprint) < metaRef.MinFingerprint ||
					metaRef.EndTimestamp.Before(params.StartTimestamp) || metaRef.StartTimestamp.After(params.EndTimestamp) {
					continue
				}
				meta, err := b.downloadMeta(ctx, metaRef, periodClient)
				if err != nil {
					return nil, err
				}
				metas = append(metas, meta)
			}
		}
	}
	return metas, nil
}

func (b *BloomClient) PutMeta(ctx context.Context, meta Meta) error {
	periodFrom, err := findPeriod(b.periodicConfigs, meta.StartTimestamp)
	if err != nil {
		return fmt.Errorf("error updloading meta file: %w", err)
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("can not marshal the meta to json: %w", err)
	}
	key := createMetaObjectKey(meta.MetaRef.Ref)
	return b.periodicObjectClients[periodFrom].PutObject(ctx, key, bytes.NewReader(data))
}

func createBlockObjectKey(meta Ref) string {
	blockParentFolder := fmt.Sprintf("%x-%x", meta.MinFingerprint, meta.MaxFingerprint)
	filename := fmt.Sprintf("%d-%d-%x", meta.StartTimestamp, meta.EndTimestamp, meta.Checksum)
	return strings.Join([]string{rootFolder, meta.TableName, meta.TenantID, bloomsFolder, blockParentFolder, filename}, delimiter)
}

func createMetaObjectKey(meta Ref) string {
	filename := fmt.Sprintf("%x-%x-%d-%d-%x", meta.MinFingerprint, meta.MaxFingerprint, meta.StartTimestamp, meta.EndTimestamp, meta.Checksum)
	return strings.Join([]string{rootFolder, meta.TableName, meta.TenantID, metasFolder, filename}, delimiter)
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
	periodFrom, err := findPeriod(b.periodicConfigs, meta.StartTimestamp)
	if err != nil {
		return err
	}
	key := createMetaObjectKey(meta.MetaRef.Ref)
	return b.periodicObjectClients[periodFrom].DeleteObject(ctx, key)
}

// GetBlock downloads the blocks from objectStorage and returns the downloaded block
func (b *BloomClient) GetBlock(ctx context.Context, reference BlockRef) (LazyBlock, error) {
	period, err := findPeriod(b.periodicConfigs, reference.StartTimestamp)
	if err != nil {
		return LazyBlock{}, fmt.Errorf("error while period lookup: %w", err)
	}
	objectClient := b.periodicObjectClients[period]
	readCloser, _, err := objectClient.GetObject(ctx, createBlockObjectKey(reference.Ref))
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
	//todo move concurrency to the config
	err := concurrency.ForEachJob(ctx, len(blocks), 100, func(ctx context.Context, idx int) error {
		block := blocks[idx]
		defer func(Data io.ReadCloser) {
			_ = Data.Close()
		}(block.Data)

		period, err := findPeriod(b.periodicConfigs, block.StartTimestamp)
		if err != nil {
			return fmt.Errorf("error uploading block file: %w", err)
		}
		key := createBlockObjectKey(block.Ref)
		objectClient := b.periodicObjectClients[period]

		_, err = block.Data.Seek(0, 0)
		if err != nil {
			return fmt.Errorf("error uploading block file: %w", err)
		}

		err = objectClient.PutObject(ctx, key, block.Data)
		if err != nil {
			return fmt.Errorf("error uploading block file: %w", err)
		}
		block.BlockPath = key
		results[idx] = block
		return nil
	})
	return results, err
}

func (b *BloomClient) DeleteBlocks(ctx context.Context, references []BlockRef) error {
	//todo move concurrency to the config
	return concurrency.ForEachJob(ctx, len(references), 100, func(ctx context.Context, idx int) error {
		ref := references[idx]
		period, err := findPeriod(b.periodicConfigs, ref.StartTimestamp)
		if err != nil {
			return fmt.Errorf("error deleting block file: %w", err)
		}
		key := createBlockObjectKey(ref.Ref)
		objectClient := b.periodicObjectClients[period]
		err = objectClient.DeleteObject(ctx, key)
		if err != nil {
			return fmt.Errorf("error deleting block file: %w", err)
		}
		return nil
	})
}

func (b *BloomClient) Stop() {
	for _, objectClient := range b.periodicObjectClients {
		objectClient.Stop()
	}
}

func (b *BloomClient) downloadMeta(ctx context.Context, metaRef MetaRef, client client.ObjectClient) (Meta, error) {
	meta := Meta{
		MetaRef: metaRef,
	}
	reader, _, err := client.GetObject(ctx, metaRef.FilePath)
	if err != nil {
		return Meta{}, fmt.Errorf("error downloading meta file %s : %w", metaRef.FilePath, err)
	}
	defer reader.Close()

	err = json.NewDecoder(reader).Decode(&meta)
	if err != nil {
		return Meta{}, fmt.Errorf("error unmarshalling content of meta file %s: %w", metaRef.FilePath, err)
	}
	return meta, nil
}

func createMetaRef(objectKey string, tenantID string, tableName string) (MetaRef, error) {
	fileName := objectKey[strings.LastIndex(objectKey, delimiter)+1:]
	parts := strings.Split(fileName, fileNamePartDelimiter)
	if len(parts) != 5 {
		return MetaRef{}, fmt.Errorf("%s filename parts count must be 5 but was %d: [%s]", objectKey, len(parts), strings.Join(parts, ", "))
	}

	minFingerprint, err := strconv.ParseUint(parts[0], 16, 64)
	if err != nil {
		return MetaRef{}, fmt.Errorf("error parsing minFingerprint %s : %w", parts[0], err)
	}
	maxFingerprint, err := strconv.ParseUint(parts[1], 16, 64)
	if err != nil {
		return MetaRef{}, fmt.Errorf("error parsing maxFingerprint %s : %w", parts[1], err)
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
			MinFingerprint: minFingerprint,
			MaxFingerprint: maxFingerprint,
			StartTimestamp: model.Time(startTimestamp),
			EndTimestamp:   model.Time(endTimestamp),
			Checksum:       uint32(checksum),
		},
		FilePath: objectKey,
	}, nil
}

func tablesByPeriod(periodicConfigs []config.PeriodConfig, start, end model.Time) map[config.DayTime][]string {
	result := make(map[config.DayTime][]string)
	for i := len(periodicConfigs) - 1; i >= 0; i-- {
		periodConfig := periodicConfigs[i]
		if end.Before(periodConfig.From.Time) {
			continue
		}
		owningPeriodStartTs := math.Max64(periodConfig.From.Unix(), start.Unix())
		owningPeriodEndTs := end.Unix()
		if i != len(periodicConfigs)-1 {
			nextPeriodConfig := periodicConfigs[i+1]
			owningPeriodEndTs = math.Min64(nextPeriodConfig.From.Add(-1*time.Second).Unix(), owningPeriodEndTs)
		}
		result[periodConfig.From] = tablesForRange(periodicConfigs[i], owningPeriodStartTs, owningPeriodEndTs)
		if !start.Before(periodConfig.From.Time) {
			break
		}
	}
	return result
}

func tablesForRange(periodConfig config.PeriodConfig, from, to int64) []string {
	interval := periodConfig.IndexTables.Period
	step := int64(interval.Seconds())
	lower := from / step
	upper := to / step
	tables := make([]string, 0, 1+upper-lower)
	prefix := periodConfig.IndexTables.Prefix
	for i := lower; i <= upper; i++ {
		tables = append(tables, joinTableName(prefix, i))
	}
	return tables
}

func joinTableName(prefix string, tableNumber int64) string {
	return fmt.Sprintf("%s%d", prefix, tableNumber)
}
