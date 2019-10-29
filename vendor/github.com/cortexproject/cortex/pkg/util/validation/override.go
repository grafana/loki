package validation

import (
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var overridesReloadSuccess = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "cortex_overrides_last_reload_successful",
	Help: "Whether the last overrides reload attempt was successful.",
})

// OverridesLoader loads the overrides
type OverridesLoader func(string) (map[string]interface{}, error)

// OverridesManagerConfig holds the config for an OverridesManager instance.
// It holds config related to loading per-tentant overrides and the default limits
type OverridesManagerConfig struct {
	OverridesReloadPeriod time.Duration
	OverridesLoadPath     string
	OverridesLoader       OverridesLoader
	Defaults              interface{}
}

// OverridesManager manages default and per user limits i.e overrides.
// It can periodically keep reloading overrides based on config.
type OverridesManager struct {
	cfg          OverridesManagerConfig
	overrides    map[string]interface{}
	overridesMtx sync.RWMutex
	quit         chan struct{}
}

// NewOverridesManager creates an instance of OverridesManager and starts reload overrides loop based on config
func NewOverridesManager(cfg OverridesManagerConfig) (*OverridesManager, error) {
	overridesManager := OverridesManager{
		cfg:  cfg,
		quit: make(chan struct{}),
	}

	if cfg.OverridesLoadPath != "" {
		if err := overridesManager.loadOverrides(); err != nil {
			// Log but don't stop on error - we don't want to halt all ingesters because of a typo
			level.Error(util.Logger).Log("msg", "failed to load limit overrides", "err", err)
		}
		go overridesManager.loop()
	} else {
		level.Info(util.Logger).Log("msg", "per-tenant overrides disabled")
	}

	return &overridesManager, nil
}

func (om *OverridesManager) loop() {
	ticker := time.NewTicker(om.cfg.OverridesReloadPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			err := om.loadOverrides()
			if err != nil {
				// Log but don't stop on error - we don't want to halt all ingesters because of a typo
				level.Error(util.Logger).Log("msg", "failed to load limit overrides", "err", err)
			}
		case <-om.quit:
			return
		}
	}
}

func (om *OverridesManager) loadOverrides() error {
	overrides, err := om.cfg.OverridesLoader(om.cfg.OverridesLoadPath)
	if err != nil {
		overridesReloadSuccess.Set(0)
		return err
	}
	overridesReloadSuccess.Set(1)

	om.overridesMtx.Lock()
	defer om.overridesMtx.Unlock()
	om.overrides = overrides
	return nil
}

// Stop stops the OverridesManager
func (om *OverridesManager) Stop() {
	close(om.quit)
}

// GetLimits returns Limits for a specific userID if its set otherwise the default Limits
func (om *OverridesManager) GetLimits(userID string) interface{} {
	om.overridesMtx.RLock()
	defer om.overridesMtx.RUnlock()

	override, ok := om.overrides[userID]
	if !ok {
		return om.cfg.Defaults
	}

	return override
}
