package bucketclient

import (
	"bytes"
	"context"
	"io/ioutil"
	"sync"

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
	"github.com/thanos-io/thanos/pkg/objstore"
	"github.com/thanos-io/thanos/pkg/runutil"

	"github.com/cortexproject/cortex/pkg/alertmanager/alertspb"
	"github.com/cortexproject/cortex/pkg/storage/bucket"
	"github.com/cortexproject/cortex/pkg/util/concurrency"
)

const (
	// The bucket prefix under which all tenants alertmanager configs are stored.
	alertsPrefix = "alerts"

	// How many users to load concurrently.
	fetchConcurrency = 16
)

// BucketAlertStore is used to support the AlertStore interface against an object storage backend. It is implemented
// using the Thanos objstore.Bucket interface
type BucketAlertStore struct {
	bucket      objstore.Bucket
	cfgProvider bucket.TenantConfigProvider
	logger      log.Logger
}

func NewBucketAlertStore(bkt objstore.Bucket, cfgProvider bucket.TenantConfigProvider, logger log.Logger) *BucketAlertStore {
	return &BucketAlertStore{
		bucket:      bucket.NewPrefixedBucketClient(bkt, alertsPrefix),
		cfgProvider: cfgProvider,
		logger:      logger,
	}
}

// ListAllUsers implements alertstore.AlertStore.
func (s *BucketAlertStore) ListAllUsers(ctx context.Context) ([]string, error) {
	var userIDs []string

	err := s.bucket.Iter(ctx, "", func(key string) error {
		userIDs = append(userIDs, key)
		return nil
	})

	return userIDs, err
}

// GetAlertConfigs implements alertstore.AlertStore.
func (s *BucketAlertStore) GetAlertConfigs(ctx context.Context, userIDs []string) (map[string]alertspb.AlertConfigDesc, error) {
	var (
		cfgsMx = sync.Mutex{}
		cfgs   = make(map[string]alertspb.AlertConfigDesc, len(userIDs))
	)

	err := concurrency.ForEach(ctx, concurrency.CreateJobsFromStrings(userIDs), fetchConcurrency, func(ctx context.Context, job interface{}) error {
		userID := job.(string)

		cfg, err := s.getAlertConfig(ctx, userID)
		if s.bucket.IsObjNotFoundErr(err) {
			return nil
		} else if err != nil {
			return errors.Wrapf(err, "failed to fetch alertmanager config for user %s", userID)
		}

		cfgsMx.Lock()
		cfgs[userID] = cfg
		cfgsMx.Unlock()

		return nil
	})

	return cfgs, err
}

// GetAlertConfig implements alertstore.AlertStore.
func (s *BucketAlertStore) GetAlertConfig(ctx context.Context, userID string) (alertspb.AlertConfigDesc, error) {
	cfg, err := s.getAlertConfig(ctx, userID)
	if s.bucket.IsObjNotFoundErr(err) {
		return cfg, alertspb.ErrNotFound
	}

	return cfg, err
}

// SetAlertConfig implements alertstore.AlertStore.
func (s *BucketAlertStore) SetAlertConfig(ctx context.Context, cfg alertspb.AlertConfigDesc) error {
	cfgBytes, err := cfg.Marshal()
	if err != nil {
		return err
	}

	return s.getUserBucket(cfg.User).Upload(ctx, cfg.User, bytes.NewBuffer(cfgBytes))
}

// DeleteAlertConfig implements alertstore.AlertStore.
func (s *BucketAlertStore) DeleteAlertConfig(ctx context.Context, userID string) error {
	userBkt := s.getUserBucket(userID)

	err := userBkt.Delete(ctx, userID)
	if userBkt.IsObjNotFoundErr(err) {
		return nil
	}
	return err
}

func (s *BucketAlertStore) getAlertConfig(ctx context.Context, userID string) (alertspb.AlertConfigDesc, error) {
	readCloser, err := s.getUserBucket(userID).Get(ctx, userID)
	if err != nil {
		return alertspb.AlertConfigDesc{}, err
	}

	defer runutil.CloseWithLogOnErr(s.logger, readCloser, "close alertmanager config reader")

	buf, err := ioutil.ReadAll(readCloser)
	if err != nil {
		return alertspb.AlertConfigDesc{}, err
	}

	config := alertspb.AlertConfigDesc{}
	err = config.Unmarshal(buf)
	if err != nil {
		return alertspb.AlertConfigDesc{}, err
	}

	return config, nil
}

func (s *BucketAlertStore) getUserBucket(userID string) objstore.Bucket {
	// Inject server-side encryption based on the tenant config.
	return bucket.NewSSEBucketClient(userID, s.bucket, s.cfgProvider)
}
