package objectclient

import (
	"bytes"
	"context"
	"io/ioutil"
	"path"

	"github.com/cortexproject/cortex/pkg/alertmanager/alerts"
	"github.com/cortexproject/cortex/pkg/chunk"
)

// Object Alert Storage Schema
// =======================
// Object Name: "alerts/<user_id>"
// Storage Format: Encoded AlertConfigDesc

const (
	alertPrefix = "alerts/"
)

// AlertStore allows cortex alertmanager configs to be stored using an object store backend.
type AlertStore struct {
	client chunk.ObjectClient
}

// NewAlertStore returns a new AlertStore
func NewAlertStore(client chunk.ObjectClient) *AlertStore {
	return &AlertStore{
		client: client,
	}
}

// ListAlertConfigs returns all of the active alert configs in this store
func (a *AlertStore) ListAlertConfigs(ctx context.Context) (map[string]alerts.AlertConfigDesc, error) {
	objs, _, err := a.client.List(ctx, alertPrefix)
	if err != nil {
		return nil, err
	}

	cfgs := map[string]alerts.AlertConfigDesc{}

	for _, obj := range objs {
		cfg, err := a.getAlertConfig(ctx, obj.Key)
		if err != nil {
			return nil, err
		}
		cfgs[cfg.User] = cfg
	}

	return cfgs, nil
}

func (a *AlertStore) getAlertConfig(ctx context.Context, key string) (alerts.AlertConfigDesc, error) {
	reader, err := a.client.GetObject(ctx, key)
	if err != nil {
		return alerts.AlertConfigDesc{}, err
	}

	buf, err := ioutil.ReadAll(reader)
	if err != nil {
		return alerts.AlertConfigDesc{}, err
	}

	config := alerts.AlertConfigDesc{}
	err = config.Unmarshal(buf)
	if err != nil {
		return alerts.AlertConfigDesc{}, err
	}

	return config, nil
}

// GetAlertConfig returns a specified user's alertmanager configuration
func (a *AlertStore) GetAlertConfig(ctx context.Context, user string) (alerts.AlertConfigDesc, error) {
	cfg, err := a.getAlertConfig(ctx, path.Join(alertPrefix, user))
	if err == chunk.ErrStorageObjectNotFound {
		return cfg, alerts.ErrNotFound
	}

	return cfg, err
}

// SetAlertConfig sets a specified user's alertmanager configuration
func (a *AlertStore) SetAlertConfig(ctx context.Context, cfg alerts.AlertConfigDesc) error {
	cfgBytes, err := cfg.Marshal()
	if err != nil {
		return err
	}

	return a.client.PutObject(ctx, path.Join(alertPrefix, cfg.User), bytes.NewReader(cfgBytes))
}

// DeleteAlertConfig deletes a specified user's alertmanager configuration
func (a *AlertStore) DeleteAlertConfig(ctx context.Context, user string) error {
	return a.client.DeleteObject(ctx, path.Join(alertPrefix, user))
}
