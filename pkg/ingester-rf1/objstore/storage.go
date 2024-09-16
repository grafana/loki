package objstore

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

type Multi struct {
	stores        []*storeEntry
	storageConfig storage.Config
}

type storeEntry struct {
	start        model.Time
	cfg          config.PeriodConfig
	objectClient client.ObjectClient
}

var _ client.ObjectClient = (*Multi)(nil)

func New(
	periodicConfigs []config.PeriodConfig,
	storageConfig storage.Config,
	clientMetrics storage.ClientMetrics,
) (*Multi, error) {
	store := &Multi{
		storageConfig: storageConfig,
	}
	// sort by From time
	sort.Slice(periodicConfigs, func(i, j int) bool {
		return periodicConfigs[i].From.Time.Before(periodicConfigs[j].From.Time)
	})
	for _, periodicConfig := range periodicConfigs {
		objectClient, err := storage.NewObjectClient(periodicConfig.ObjectType, storageConfig, clientMetrics)
		if err != nil {
			return nil, fmt.Errorf("creating object client for period %s: %w ", periodicConfig.From, err)
		}
		store.stores = append(store.stores, &storeEntry{
			start:        periodicConfig.From.Time,
			cfg:          periodicConfig,
			objectClient: objectClient,
		})
	}
	return store, nil
}

func (m *Multi) GetStoreFor(ts model.Time) (client.ObjectClient, error) {
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

func (m *Multi) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	s, err := m.GetStoreFor(model.Now())
	if err != nil {
		return false, err
	}
	return s.ObjectExists(ctx, objectKey)
}

func (m *Multi) PutObject(ctx context.Context, objectKey string, object io.Reader) error {
	s, err := m.GetStoreFor(model.Now())
	if err != nil {
		return err
	}
	return s.PutObject(ctx, objectKey, object)
}

func (m *Multi) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	s, err := m.GetStoreFor(model.Now())
	if err != nil {
		return nil, 0, err
	}
	return s.GetObject(ctx, objectKey)
}

func (m *Multi) GetObjectRange(ctx context.Context, objectKey string, off, length int64) (io.ReadCloser, error) {
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

func (m *Multi) List(ctx context.Context, prefix string, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	s, err := m.GetStoreFor(model.Now())
	if err != nil {
		return nil, nil, err
	}
	return s.List(ctx, prefix, delimiter)
}

func (m *Multi) DeleteObject(ctx context.Context, objectKey string) error {
	s, err := m.GetStoreFor(model.Now())
	if err != nil {
		return err
	}
	return s.DeleteObject(ctx, objectKey)
}

func (m *Multi) IsObjectNotFoundErr(err error) bool {
	s, _ := m.GetStoreFor(model.Now())
	if s == nil {
		return false
	}
	return s.IsObjectNotFoundErr(err)
}

func (m *Multi) IsRetryableErr(err error) bool {
	s, _ := m.GetStoreFor(model.Now())
	if s == nil {
		return false
	}
	return s.IsRetryableErr(err)
}

func (m *Multi) Stop() {
	for _, s := range m.stores {
		s.objectClient.Stop()
	}
}
