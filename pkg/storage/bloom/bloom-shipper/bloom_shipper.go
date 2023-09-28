package bloom_shipper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/util/math"
	"github.com/prometheus/common/model"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	metasFolder           = "metas"
	delimiter             = "/"
	fileNamePartDelimiter = "-"
)

type BlockRef struct {
	TenantID      string
	TSDBSource    string
	BlockFilePath string
	//todo check if it should be string
	Checksum string

	MinFingerprint, MaxFingerprint uint64
	StartTimestamp, EndTimestamp   int64
}

type MetaRef struct {
	TenantID                       string
	TableName                      string
	MinFingerprint, MaxFingerprint uint64
	// check if it has to be time.Time
	StartTimestamp, EndTimestamp int64
	//todo check if it should be string
	Checksum string
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

type MetaShipper interface {
	// Returns all metas that are within MinFingerprint-MaxFingerprint fingerprint range
	// and intersect time period from StartTimestamp to EndTimestamp.
	GetAll(ctx context.Context, params MetaSearchParams) ([]Meta, error)
	Upload(ctx context.Context, meta Meta) error
	Delete(ctx context.Context, meta Meta) error
}

type Block struct {
	BlockRef
	//todo TBD
	Data []byte
}

type BlockShipper interface {
	GetBlocks(ctx context.Context, references []BlockRef) ([]Block, error)
	UploadBlocks(ctx context.Context, blocks []Block) error
	DeleteBlocks(ctx context.Context, blocks []BlockRef) error
}

type Shipper interface {
	MetaShipper
	BlockShipper
	Stop()
}

// todo add logger
func NewShipper(periodicConfigs []config.PeriodConfig, storageConfig storage.Config, clientMetrics storage.ClientMetrics) (Shipper, error) {
	periodicObjectClients := make(map[config.DayTime]client.ObjectClient)
	for _, periodicConfig := range periodicConfigs {
		objectClient, err := storage.NewObjectClient(periodicConfig.ObjectType, storageConfig, clientMetrics)
		if err != nil {
			return nil, fmt.Errorf("error creating object client '%s': %w", periodicConfig.ObjectType, err)
		}
		periodicObjectClients[periodicConfig.From] = objectClient
	}
	return &bloomShipper{
		periodicConfigs:       periodicConfigs,
		storageConfig:         storageConfig,
		periodicObjectClients: periodicObjectClients,
	}, nil
}

type bloomShipper struct {
	periodicConfigs       []config.PeriodConfig
	storageConfig         storage.Config
	periodicObjectClients map[config.DayTime]client.ObjectClient
}

func (b *bloomShipper) GetAll(ctx context.Context, params MetaSearchParams) ([]Meta, error) {
	start := model.TimeFromUnix(params.StartTimestamp)
	end := model.TimeFromUnix(params.EndTimestamp)
	tablesByPeriod := tablesByPeriod(b.periodicConfigs, start, end)

	var metas []Meta
	for periodFrom, tables := range tablesByPeriod {
		periodClient := b.periodicObjectClients[periodFrom]
		for _, table := range tables {
			prefix := filepath.Join(table, params.TenantID, metasFolder)
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

func (b *bloomShipper) Upload(ctx context.Context, meta Meta) error {
	periodFrom, err := findPeriod(b.periodicConfigs, meta.StartTimestamp)
	if err != nil {
		return fmt.Errorf("error updloading meta file: %w", err)
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("can not marshal the meta to json: %w", err)
	}
	key := createObjectKey(meta.MetaRef)
	fmt.Println("uploading to ", key, "periodfrom", periodFrom)
	return b.periodicObjectClients[periodFrom].PutObject(ctx, key, bytes.NewReader(data))
}

func createObjectKey(meta MetaRef) string {
	filename := fmt.Sprintf("%x-%x-%v-%v-%s", meta.MinFingerprint, meta.MaxFingerprint, meta.StartTimestamp, meta.EndTimestamp, meta.Checksum)
	return strings.Join([]string{meta.TableName, meta.TenantID, metasFolder, filename}, delimiter)
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
func (b *bloomShipper) Delete(ctx context.Context, meta Meta) error {
	periodFrom, err := findPeriod(b.periodicConfigs, meta.StartTimestamp)
	if err != nil {
		return fmt.Errorf("error updloading meta file: %w", err)
	}
	key := createObjectKey(meta.MetaRef)
	fmt.Println("uploading to ", key, "periodfrom", periodFrom)
	return b.periodicObjectClients[periodFrom].DeleteObject(ctx, key)
}
func (b *bloomShipper) GetBlocks(ctx context.Context, references []BlockRef) ([]Block, error) {
	return nil, nil
}
func (b *bloomShipper) UploadBlocks(ctx context.Context, blocks []Block) error {
	return nil
}
func (b *bloomShipper) DeleteBlocks(ctx context.Context, blocks []BlockRef) error {
	return nil
}

func (b *bloomShipper) Stop() {
	for _, objectClient := range b.periodicObjectClients {
		objectClient.Stop()
	}
}

func (b *bloomShipper) downloadMeta(ctx context.Context, metaRef MetaRef, client client.ObjectClient) (Meta, error) {
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
		return MetaRef{}, fmt.Errorf("%s filename parts count must be 5 but was %d", objectKey, len(parts))
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
	checksum := parts[4]
	return MetaRef{
		TenantID:       tenantID,
		TableName:      tableName,
		MinFingerprint: minFingerprint,
		MaxFingerprint: maxFingerprint,
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
		Checksum:       checksum,
		FilePath:       objectKey,
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
	fmt.Println(tables)
	return tables
}

func joinTableName(prefix string, tableNumber int64) string {
	return fmt.Sprintf("%s%d", prefix, tableNumber)
}
