package objectclient

import (
	"bytes"
	"context"
	"io/ioutil"
	"path"
	"strings"
	"sync"

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
	"github.com/thanos-io/thanos/pkg/runutil"

	"github.com/cortexproject/cortex/pkg/alertmanager/alertspb"
	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/util/concurrency"
)

// Object Alert Storage Schema
// =======================
// Object Name: "alerts/<user_id>"
// Storage Format: Encoded AlertConfigDesc

const (
	// The bucket prefix under which all tenants alertmanager configs are stored.
	alertPrefix = "alerts/"

	// How many users to load concurrently.
	fetchConcurrency = 16
)

var (
	errState = errors.New("legacy object alertmanager storage does not support state persistency")
)

// AlertStore allows cortex alertmanager configs to be stored using an object store backend.
type AlertStore struct {
	client chunk.ObjectClient
	logger log.Logger
}

// NewAlertStore returns a new AlertStore
func NewAlertStore(client chunk.ObjectClient, logger log.Logger) *AlertStore {
	return &AlertStore{
		client: client,
		logger: logger,
	}
}

// ListAllUsers implements alertstore.AlertStore.
func (a *AlertStore) ListAllUsers(ctx context.Context) ([]string, error) {
	objs, _, err := a.client.List(ctx, alertPrefix, "")
	if err != nil {
		return nil, err
	}

	userIDs := make([]string, 0, len(objs))
	for _, obj := range objs {
		userID := strings.TrimPrefix(obj.Key, alertPrefix)
		userIDs = append(userIDs, userID)
	}

	return userIDs, nil
}

// GetAlertConfigs implements alertstore.AlertStore.
func (a *AlertStore) GetAlertConfigs(ctx context.Context, userIDs []string) (map[string]alertspb.AlertConfigDesc, error) {
	var (
		cfgsMx = sync.Mutex{}
		cfgs   = make(map[string]alertspb.AlertConfigDesc, len(userIDs))
	)

	err := concurrency.ForEach(ctx, concurrency.CreateJobsFromStrings(userIDs), fetchConcurrency, func(ctx context.Context, job interface{}) error {
		userID := job.(string)

		cfg, err := a.getAlertConfig(ctx, path.Join(alertPrefix, userID))
		if errors.Is(err, chunk.ErrStorageObjectNotFound) {
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

func (a *AlertStore) getAlertConfig(ctx context.Context, key string) (alertspb.AlertConfigDesc, error) {
	readCloser, err := a.client.GetObject(ctx, key)
	if err != nil {
		return alertspb.AlertConfigDesc{}, err
	}

	defer runutil.CloseWithLogOnErr(a.logger, readCloser, "close alert config reader")

	buf, err := ioutil.ReadAll(readCloser)
	if err != nil {
		return alertspb.AlertConfigDesc{}, errors.Wrapf(err, "failed to read alertmanager config %s", key)
	}

	config := alertspb.AlertConfigDesc{}
	err = config.Unmarshal(buf)
	if err != nil {
		return alertspb.AlertConfigDesc{}, errors.Wrapf(err, "failed to unmarshal alertmanager config %s", key)
	}

	return config, nil
}

// GetAlertConfig implements alertstore.AlertStore.
func (a *AlertStore) GetAlertConfig(ctx context.Context, user string) (alertspb.AlertConfigDesc, error) {
	cfg, err := a.getAlertConfig(ctx, path.Join(alertPrefix, user))
	if err == chunk.ErrStorageObjectNotFound {
		return cfg, alertspb.ErrNotFound
	}

	return cfg, err
}

// SetAlertConfig implements alertstore.AlertStore.
func (a *AlertStore) SetAlertConfig(ctx context.Context, cfg alertspb.AlertConfigDesc) error {
	cfgBytes, err := cfg.Marshal()
	if err != nil {
		return err
	}

	return a.client.PutObject(ctx, path.Join(alertPrefix, cfg.User), bytes.NewReader(cfgBytes))
}

// DeleteAlertConfig implements alertstore.AlertStore.
func (a *AlertStore) DeleteAlertConfig(ctx context.Context, user string) error {
	err := a.client.DeleteObject(ctx, path.Join(alertPrefix, user))
	if err == chunk.ErrStorageObjectNotFound {
		return nil
	}
	return err
}

// ListUsersWithFullState implements alertstore.AlertStore.
func (a *AlertStore) ListUsersWithFullState(ctx context.Context) ([]string, error) {
	return nil, errState
}

// GetFullState implements alertstore.AlertStore.
func (a *AlertStore) GetFullState(ctx context.Context, user string) (alertspb.FullStateDesc, error) {
	return alertspb.FullStateDesc{}, errState
}

// SetFullState implements alertstore.AlertStore.
func (a *AlertStore) SetFullState(ctx context.Context, user string, cfg alertspb.FullStateDesc) error {
	return errState
}

// DeleteFullState implements alertstore.AlertStore.
func (a *AlertStore) DeleteFullState(ctx context.Context, user string) error {
	return errState
}
