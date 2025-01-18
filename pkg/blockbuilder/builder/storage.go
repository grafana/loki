package builder

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

type MultiStore struct {
	stores        []*storeEntry
	storageConfig storage.Config
}

type storeEntry struct {
	start        model.Time
	cfg          config.PeriodConfig
	objectClient client.ObjectClient
}

var _ client.ObjectClient = (*MultiStore)(nil)

func NewMultiStore(
	periodicConfigs []config.PeriodConfig,
	storageConfig storage.Config,
	clientMetrics storage.ClientMetrics,
) (*MultiStore, error) {
	store := &MultiStore{
		storageConfig: storageConfig,
	}
	// sort by From time
	sort.Slice(periodicConfigs, func(i, j int) bool {
		return periodicConfigs[i].From.Time.Before(periodicConfigs[j].From.Time)
	})
	for _, periodicConfig := range periodicConfigs {
		objectClient, err := storage.NewObjectClient(periodicConfig.ObjectType, "storage-rf1", storageConfig, clientMetrics)
		if err != nil {
			return nil, fmt.Errorf("creating object client for period %s: %w ", periodicConfig.From, err)
		}
		prefixed := client.NewPrefixedObjectClient(objectClient, periodicConfig.IndexTables.PathPrefix)
		store.stores = append(store.stores, &storeEntry{
			start:        periodicConfig.From.Time,
			cfg:          periodicConfig,
			objectClient: prefixed,
		})
	}
	return store, nil
}

func (m *MultiStore) GetStoreFor(ts model.Time) (client.ObjectClient, error) {
	// find the schema with the lowest start _after_ tm
	j := sort.Search(len(m.stores), func(j int) bool {
		return m.stores[j].start > ts
	})

	// reduce it by 1 because we want a schema with start <= tm
	j--

	if 0 <= j && j < len(m.stores) {
		return m.stores[j].objectClient, nil
	}

	// should in theory never happen
	return nil, fmt.Errorf("no store found for timestamp %s", ts)
}

func (m *MultiStore) GetAttributes(ctx context.Context, objectKey string) (client.ObjectAttributes, error) {
	s, err := m.GetStoreFor(model.Now())
	if err != nil {
		return client.ObjectAttributes{}, err
	}
	return s.GetAttributes(ctx, objectKey)
}

func (m *MultiStore) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	s, err := m.GetStoreFor(model.Now())
	if err != nil {
		return false, err
	}
	return s.ObjectExists(ctx, objectKey)
}

func (m *MultiStore) PutObject(ctx context.Context, objectKey string, object io.Reader) error {
	s, err := m.GetStoreFor(model.Now())
	if err != nil {
		return err
	}
	return s.PutObject(ctx, objectKey, object)
}

func (m *MultiStore) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	s, err := m.GetStoreFor(model.Now())
	if err != nil {
		return nil, 0, err
	}
	return s.GetObject(ctx, objectKey)
}

func (m *MultiStore) GetObjectRange(ctx context.Context, objectKey string, off, length int64) (io.ReadCloser, error) {
	sp, _ := opentracing.StartSpanFromContext(ctx, "GetObjectRange")
	if sp != nil {
		sp.LogKV("objectKey", objectKey, "off", off, "length", length)
	}
	defer sp.Finish()
	s, err := m.GetStoreFor(model.Now())
	if err != nil {
		return nil, err
	}
	return s.GetObjectRange(ctx, objectKey, off, length)
}

func (m *MultiStore) List(ctx context.Context, prefix string, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	s, err := m.GetStoreFor(model.Now())
	if err != nil {
		return nil, nil, err
	}
	return s.List(ctx, prefix, delimiter)
}

func (m *MultiStore) DeleteObject(ctx context.Context, objectKey string) error {
	s, err := m.GetStoreFor(model.Now())
	if err != nil {
		return err
	}
	return s.DeleteObject(ctx, objectKey)
}

func (m *MultiStore) IsObjectNotFoundErr(err error) bool {
	s, _ := m.GetStoreFor(model.Now())
	if s == nil {
		return false
	}
	return s.IsObjectNotFoundErr(err)
}

func (m *MultiStore) IsRetryableErr(err error) bool {
	s, _ := m.GetStoreFor(model.Now())
	if s == nil {
		return false
	}
	return s.IsRetryableErr(err)
}

func (m *MultiStore) Stop() {
	for _, s := range m.stores {
		s.objectClient.Stop()
	}
}
