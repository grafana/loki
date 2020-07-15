package local

import (
	"context"
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"github.com/prometheus/alertmanager/config"

	"github.com/cortexproject/cortex/pkg/alertmanager/alerts"
)

var (
	errReadOnly = errors.New("local alertmanager config storage is read-only")
)

// StoreConfig configures a static file alertmanager store
type StoreConfig struct {
	Path string `yaml:"path"`
}

// RegisterFlags registers flags related to the alertmanager file store
func (cfg *StoreConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Path, "alertmanager.storage.local.path", "", "Path at which alertmanager configurations are stored.")
}

// Store is used to load user alertmanager configs from a local disk
type Store struct {
	cfg StoreConfig
}

// NewStore returns a new file alert store.
func NewStore(cfg StoreConfig) (*Store, error) {
	return &Store{cfg}, nil
}

// ListAlertConfigs returns a list of each users alertmanager config.
func (f *Store) ListAlertConfigs(ctx context.Context) (map[string]alerts.AlertConfigDesc, error) {
	configs := map[string]alerts.AlertConfigDesc{}
	err := filepath.Walk(f.cfg.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return errors.Wrap(err, "unable to walk file path")
		}

		// Ignore files that are directories or not yaml files
		ext := filepath.Ext(info.Name())
		if info.IsDir() || (ext != ".yml" && ext != ".yaml") {
			return nil
		}

		// Ensure the file is a valid Alertmanager Config.
		_, err = config.LoadFile(path)
		if err != nil {
			return errors.Wrap(err, "unable to load file "+path)
		}

		// Load the file to be returned by the store.
		content, err := ioutil.ReadFile(path)
		if err != nil {
			return errors.Wrap(err, "unable to read file "+path)
		}

		// The file name must correspond to the user tenant ID
		user := strings.TrimSuffix(info.Name(), ext)

		configs[user] = alerts.AlertConfigDesc{
			User:      user,
			RawConfig: string(content),
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return configs, nil
}

func (f *Store) GetAlertConfig(ctx context.Context, user string) (alerts.AlertConfigDesc, error) {
	cfgs, err := f.ListAlertConfigs(ctx)
	if err != nil {
		return alerts.AlertConfigDesc{}, err
	}

	cfg, exists := cfgs[user]

	if !exists {
		return alerts.AlertConfigDesc{}, alerts.ErrNotFound
	}

	return cfg, nil
}

func (f *Store) SetAlertConfig(ctx context.Context, cfg alerts.AlertConfigDesc) error {
	return errReadOnly
}

func (f *Store) DeleteAlertConfig(ctx context.Context, user string) error {
	return errReadOnly
}
