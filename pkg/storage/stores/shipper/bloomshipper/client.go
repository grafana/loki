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

	"github.com/prometheus/common/model"

	"github.com/grafana/dskit/concurrency"

	"github.com/grafana/loki/pkg/storage"
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
	StartTimestamp, EndTimestamp   int64
	Checksum                       uint32
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
	TenantID       string
	MinFingerprint uint64
	MaxFingerprint uint64
	StartTimestamp int64
	EndTimestamp   int64
}

type MetaClient interface {
	// Returns all metas that are within MinFingerprint-MaxFingerprint fingerprint range
	// and intersect time period from StartTimestamp to EndTimestamp.
	GetMetas(ctx context.Context, params MetaSearchParams) ([]Meta, error)
	PutMeta(ctx context.Context, meta Meta) (Meta, error)
	DeleteMeta(ctx context.Context, meta Meta) error
}

type Block struct {
	BlockRef

	Data io.ReadCloser
}

type BlockClient interface {
	GetBlocks(ctx context.Context, references []BlockRef) (chan Block, chan error)
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
	start := model.TimeFromUnix(params.StartTimestamp)
	end := model.TimeFromUnix(params.EndTimestamp)
	tablesByPeriod := tablesByPeriod(b.periodicConfigs, start, end)

	var metas []Meta
	for periodFrom, tables := range tablesByPeriod {
		periodClient := b.periodicObjectClients[periodFrom]
		for _, table := range tables {
			prefix := filepath.Join(rootFolder, table, params.TenantID, metasFolder)
			list, _, err := periodClient.List(ctx, prefix, delimiter)
			if err != nil {
				return nil, fmt.Errorf("error listing metas under prefix [%s]: %w", prefix, err)
			}
			for _, object := range list {
				metaRef, err := createMetaRef(object.Key, params.TenantID, table)
				if err != nil {
					return nil, err
				}
				if metaRef.MaxFingerprint < params.MinFingerprint || params.MaxFingerprint < metaRef.MinFingerprint ||
					metaRef.StartTimestamp < params.StartTimestamp || params.EndTimestamp < metaRef.EndTimestamp {
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
	filename := fmt.Sprintf("%v-%v-%x", meta.StartTimestamp, meta.EndTimestamp, meta.Checksum)
	return strings.Join([]string{rootFolder, meta.TableName, meta.TenantID, bloomsFolder, blockParentFolder, filename}, delimiter)
}

func createMetaObjectKey(meta Ref) string {
	filename := fmt.Sprintf("%x-%x-%v-%v-%x", meta.MinFingerprint, meta.MaxFingerprint, meta.StartTimestamp, meta.EndTimestamp, meta.Checksum)
	return strings.Join([]string{rootFolder, meta.TableName, meta.TenantID, metasFolder, filename}, delimiter)
}

func findPeriod(configs []config.PeriodConfig, timestamp int64) (config.DayTime, error) {
	ts := model.TimeFromUnix(timestamp)
	for i := len(configs) - 1; i >= 0; i-- {
		periodConfig := configs[i]
		if periodConfig.From.Before(ts) || periodConfig.From.Equal(ts) {
			return periodConfig.From, nil
		}
	}
	return config.DayTime{}, fmt.Errorf("can not find period for timestamp %d", timestamp)
}
func (b *BloomClient) DeleteMeta(ctx context.Context, meta Meta) error {
	periodFrom, err := findPeriod(b.periodicConfigs, meta.StartTimestamp)
	if err != nil {
		return fmt.Errorf("error updloading meta file: %w", err)
	}
	key := createMetaObjectKey(meta.MetaRef.Ref)
	return b.periodicObjectClients[periodFrom].DeleteObject(ctx, key)
}

// GetBlocks downloads all the blocks from objectStorage in parallel and sends the downloaded blocks
// via the channel Block that is closed only if all the blocks are downloaded without errors.
// If an error happens, the error will be sent via error channel.
func (b *BloomClient) GetBlocks(ctx context.Context, references []BlockRef) (chan Block, chan error) {
	blocksChannel := make(chan Block, len(references))
	errChannel := make(chan error)
	go func() {
		//todo move concurrency to the config
		err := concurrency.ForEachJob(ctx, len(references), 100, func(ctx context.Context, idx int) error {
			reference := references[idx]
			period, err := findPeriod(b.periodicConfigs, reference.StartTimestamp)
			if err != nil {
				return fmt.Errorf("error while period lookup: %w", err)
			}
			objectClient := b.periodicObjectClients[period]
			readCloser, _, err := objectClient.GetObject(ctx, createBlockObjectKey(reference.Ref))
			if err != nil {
				return fmt.Errorf("error while fetching object from storage: %w", err)
			}
			blocksChannel <- Block{
				BlockRef: reference,
				Data:     readCloser,
			}
			return nil
		})
		if err != nil {
			errChannel <- fmt.Errorf("error downloading block file: %w", err)
			return
		}
		//close blocks channel only if there is no error
		close(blocksChannel)
	}()
	return blocksChannel, errChannel
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
			return fmt.Errorf("error updloading block file: %w", err)
		}
		key := createBlockObjectKey(block.Ref)
		objectClient := b.periodicObjectClients[period]
		data, err := io.ReadAll(block.Data)
		if err != nil {
			return fmt.Errorf("error while reading object data: %w", err)
		}
		err = objectClient.PutObject(ctx, key, bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("error updloading block file: %w", err)
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
	defer func() { _ = reader.Close() }()

	buf, err := io.ReadAll(reader)
	if err != nil {
		return Meta{}, fmt.Errorf("error reading meta file %s: %w", metaRef.FilePath, err)
	}
	err = json.Unmarshal(buf, &meta)
	if err != nil {
		return Meta{}, fmt.Errorf("error unmarshalling content of meta file %s: %w", metaRef.FilePath, err)
	}
	return meta, nil
}

// todo cover with tests
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
			StartTimestamp: startTimestamp,
			EndTimestamp:   endTimestamp,
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
	intervalSeconds := interval.Seconds()
	lower := from / int64(intervalSeconds)
	upper := to / int64(intervalSeconds)
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
