package memory

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/configs/userconfig"
)

// DB is an in-memory database for testing, and local development
type DB struct {
	cfgs map[string]userconfig.View
	id   uint
}

// New creates a new in-memory database
func New(_, _ string) (*DB, error) {
	return &DB{
		cfgs: map[string]userconfig.View{},
		id:   0,
	}, nil
}

// GetConfig gets the user's configuration.
func (d *DB) GetConfig(ctx context.Context, userID string) (userconfig.View, error) {
	c, ok := d.cfgs[userID]
	if !ok {
		return userconfig.View{}, sql.ErrNoRows
	}
	return c, nil
}

// SetConfig sets configuration for a user.
func (d *DB) SetConfig(ctx context.Context, userID string, cfg userconfig.Config) error {
	if !cfg.RulesConfig.FormatVersion.IsValid() {
		return fmt.Errorf("invalid rule format version %v", cfg.RulesConfig.FormatVersion)
	}
	d.cfgs[userID] = userconfig.View{Config: cfg, ID: userconfig.ID(d.id)}
	d.id++
	return nil
}

// GetAllConfigs gets all of the userconfig.
func (d *DB) GetAllConfigs(ctx context.Context) (map[string]userconfig.View, error) {
	return d.cfgs, nil
}

// GetConfigs gets all of the configs that have changed recently.
func (d *DB) GetConfigs(ctx context.Context, since userconfig.ID) (map[string]userconfig.View, error) {
	cfgs := map[string]userconfig.View{}
	for user, c := range d.cfgs {
		if c.ID > since {
			cfgs[user] = c
		}
	}
	return cfgs, nil
}

// SetDeletedAtConfig sets a deletedAt for configuration
// by adding a single new row with deleted_at set
// the same as SetConfig is actually insert
func (d *DB) SetDeletedAtConfig(ctx context.Context, userID string, deletedAt time.Time) error {
	cv, err := d.GetConfig(ctx, userID)
	if err != nil {
		return err
	}
	cv.DeletedAt = deletedAt
	cv.ID = userconfig.ID(d.id)
	d.cfgs[userID] = cv
	d.id++
	return nil
}

// DeactivateConfig deactivates configuration for a user by creating new configuration with DeletedAt set to now
func (d *DB) DeactivateConfig(ctx context.Context, userID string) error {
	return d.SetDeletedAtConfig(ctx, userID, time.Now())
}

// RestoreConfig restores deactivated configuration for a user by creating new configuration with empty DeletedAt
func (d *DB) RestoreConfig(ctx context.Context, userID string) error {
	return d.SetDeletedAtConfig(ctx, userID, time.Time{})
}

// Close finishes using the db. Noop.
func (d *DB) Close() error {
	return nil
}

// GetRulesConfig gets the rules config for a user.
func (d *DB) GetRulesConfig(ctx context.Context, userID string) (userconfig.VersionedRulesConfig, error) {
	c, ok := d.cfgs[userID]
	if !ok {
		return userconfig.VersionedRulesConfig{}, sql.ErrNoRows
	}
	cfg := c.GetVersionedRulesConfig()
	if cfg == nil {
		return userconfig.VersionedRulesConfig{}, sql.ErrNoRows
	}
	return *cfg, nil
}

// SetRulesConfig sets the rules config for a user.
func (d *DB) SetRulesConfig(ctx context.Context, userID string, oldConfig, newConfig userconfig.RulesConfig) (bool, error) {
	c, ok := d.cfgs[userID]
	if !ok {
		return true, d.SetConfig(ctx, userID, userconfig.Config{RulesConfig: newConfig})
	}
	if !oldConfig.Equal(c.Config.RulesConfig) {
		return false, nil
	}
	return true, d.SetConfig(ctx, userID, userconfig.Config{
		AlertmanagerConfig: c.Config.AlertmanagerConfig,
		RulesConfig:        newConfig,
	})
}

// GetAllRulesConfigs gets the rules configs for all users that have them.
func (d *DB) GetAllRulesConfigs(ctx context.Context) (map[string]userconfig.VersionedRulesConfig, error) {
	cfgs := map[string]userconfig.VersionedRulesConfig{}
	for user, c := range d.cfgs {
		cfg := c.GetVersionedRulesConfig()
		if cfg != nil {
			cfgs[user] = *cfg
		}
	}
	return cfgs, nil
}

// GetRulesConfigs gets the rules configs that have changed
// since the given config version.
func (d *DB) GetRulesConfigs(ctx context.Context, since userconfig.ID) (map[string]userconfig.VersionedRulesConfig, error) {
	cfgs := map[string]userconfig.VersionedRulesConfig{}
	for user, c := range d.cfgs {
		if c.ID <= since {
			continue
		}
		cfg := c.GetVersionedRulesConfig()
		if cfg != nil {
			cfgs[user] = *cfg
		}
	}
	return cfgs, nil
}
