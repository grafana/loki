package bucketclient

import (
	"bytes"
	"context"
	"io/ioutil"
	"strings"
	"sync"

	"github.com/go-kit/kit/log"
	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"
	"github.com/thanos-io/thanos/pkg/objstore"
	"github.com/thanos-io/thanos/pkg/runutil"

	"github.com/cortexproject/cortex/pkg/alertmanager/alertspb"
	"github.com/cortexproject/cortex/pkg/storage/bucket"
	"github.com/cortexproject/cortex/pkg/util/concurrency"
)

const (
	// The bucket prefix under which all tenants alertmanager configs are stored.
	// Note that objects stored under this prefix follow the pattern:
	//     alerts/<user-id>
	alertsPrefix = "alerts"

	// The bucket prefix under which other alertmanager state is stored.
	// Note that objects stored under this prefix follow the pattern:
	//     alertmanager/<user-id>/<object>
	alertmanagerPrefix = "alertmanager"

	// The name of alertmanager full state objects (notification log + silences).
	fullStateName = "fullstate"

	// How many users to load concurrently.
	fetchConcurrency = 16
)

// BucketAlertStore is used to support the AlertStore interface against an object storage backend. It is implemented
// using the Thanos objstore.Bucket interface
type BucketAlertStore struct {
	alertsBucket objstore.Bucket
	amBucket     objstore.Bucket
	cfgProvider  bucket.TenantConfigProvider
	logger       log.Logger
}

func NewBucketAlertStore(bkt objstore.Bucket, cfgProvider bucket.TenantConfigProvider, logger log.Logger) *BucketAlertStore {
	return &BucketAlertStore{
		alertsBucket: bucket.NewPrefixedBucketClient(bkt, alertsPrefix),
		amBucket:     bucket.NewPrefixedBucketClient(bkt, alertmanagerPrefix),
		cfgProvider:  cfgProvider,
		logger:       logger,
	}
}

// ListAllUsers implements alertstore.AlertStore.
func (s *BucketAlertStore) ListAllUsers(ctx context.Context) ([]string, error) {
	var userIDs []string

	err := s.alertsBucket.Iter(ctx, "", func(key string) error {
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
		if s.alertsBucket.IsObjNotFoundErr(err) {
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
	if s.alertsBucket.IsObjNotFoundErr(err) {
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

// ListUsersWithFullState implements alertstore.AlertStore.
func (s *BucketAlertStore) ListUsersWithFullState(ctx context.Context) ([]string, error) {
	var userIDs []string

	err := s.amBucket.Iter(ctx, "", func(key string) error {
		userIDs = append(userIDs, strings.TrimRight(key, "/"))
		return nil
	})

	return userIDs, err
}

// GetFullState implements alertstore.AlertStore.
func (s *BucketAlertStore) GetFullState(ctx context.Context, userID string) (alertspb.FullStateDesc, error) {
	bkt := s.getAlertmanagerUserBucket(userID)
	fs := alertspb.FullStateDesc{}

	err := s.get(ctx, bkt, fullStateName, &fs)
	if s.amBucket.IsObjNotFoundErr(err) {
		return fs, alertspb.ErrNotFound
	}

	return fs, err
}

// SetFullState implements alertstore.AlertStore.
func (s *BucketAlertStore) SetFullState(ctx context.Context, userID string, fs alertspb.FullStateDesc) error {
	bkt := s.getAlertmanagerUserBucket(userID)

	fsBytes, err := fs.Marshal()
	if err != nil {
		return err
	}

	return bkt.Upload(ctx, fullStateName, bytes.NewBuffer(fsBytes))
}

// DeleteFullState implements alertstore.AlertStore.
func (s *BucketAlertStore) DeleteFullState(ctx context.Context, userID string) error {
	userBkt := s.getAlertmanagerUserBucket(userID)

	err := userBkt.Delete(ctx, fullStateName)
	if userBkt.IsObjNotFoundErr(err) {
		return nil
	}
	return err
}

func (s *BucketAlertStore) getAlertConfig(ctx context.Context, userID string) (alertspb.AlertConfigDesc, error) {
	config := alertspb.AlertConfigDesc{}
	err := s.get(ctx, s.getUserBucket(userID), userID, &config)
	return config, err
}

func (s *BucketAlertStore) get(ctx context.Context, bkt objstore.Bucket, name string, msg proto.Message) error {
	readCloser, err := bkt.Get(ctx, name)
	if err != nil {
		return err
	}

	defer runutil.CloseWithLogOnErr(s.logger, readCloser, "close bucket reader")

	buf, err := ioutil.ReadAll(readCloser)
	if err != nil {
		return errors.Wrapf(err, "failed to read alertmanager config for user %s", name)
	}

	err = proto.Unmarshal(buf, msg)
	if err != nil {
		return errors.Wrapf(err, "failed to deserialize alertmanager config for user %s", name)
	}

	return nil
}

func (s *BucketAlertStore) getUserBucket(userID string) objstore.Bucket {
	// Inject server-side encryption based on the tenant config.
	return bucket.NewSSEBucketClient(userID, s.alertsBucket, s.cfgProvider)
}

func (s *BucketAlertStore) getAlertmanagerUserBucket(userID string) objstore.Bucket {
	return bucket.NewUserBucketClient(userID, s.amBucket, s.cfgProvider).WithExpectedErrs(s.amBucket.IsObjNotFoundErr)
}
