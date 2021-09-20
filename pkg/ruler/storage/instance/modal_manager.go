package instance

import (
	"fmt"
	"sync"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Mode controls how instances are created.
type Mode string

// Types of instance modes
var (
	ModeDistinct Mode = "distinct"
	ModeShared   Mode = "shared"

	DefaultMode = ModeShared
)

// UnmarshalYAML unmarshals a string to a Mode. Fails if the string is
// unrecognized.
func (m *Mode) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*m = DefaultMode

	var plain string
	if err := unmarshal(&plain); err != nil {
		return err
	}

	switch plain {
	case string(ModeDistinct):
		*m = ModeDistinct
		return nil
	case string(ModeShared):
		*m = ModeShared
		return nil
	default:
		return fmt.Errorf("unsupported instance_mode '%s'. supported values 'shared', 'distinct'", plain)
	}
}

// ModalManager runs instances by either grouping them or running them fully
// separately.
type ModalManager struct {
	mut     sync.RWMutex
	mode    Mode
	configs map[string]Config

	changedConfigs       *prometheus.GaugeVec
	currentActiveConfigs prometheus.Gauge

	log log.Logger

	// The ModalManager wraps around a "final" Manager that is intended to
	// launch and manage instances based on Configs. This is specified here by the
	// "wrapped" Manager.
	//
	// However, there may be another manager performing formations on the configs
	// before they are passed through to wrapped. This is specified by the "active"
	// Manager.
	//
	// If no transformations on Configs are needed, active will be identical to
	// wrapped.
	wrapped, active Manager
}

// NewModalManager creates a new ModalManager.
func NewModalManager(reg prometheus.Registerer, l log.Logger, next Manager, mode Mode) (*ModalManager, error) {
	changedConfigs := promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Name: "agent_prometheus_configs_changed_total",
		Help: "Total number of dynamically updated configs",
	}, []string{"event"})
	currentActiveConfigs := promauto.With(reg).NewGauge(prometheus.GaugeOpts{
		Name: "agent_prometheus_active_configs",
		Help: "Current number of active configs being used by the agent.",
	})

	mm := ModalManager{
		wrapped:              next,
		log:                  l,
		changedConfigs:       changedConfigs,
		currentActiveConfigs: currentActiveConfigs,
		configs:              make(map[string]Config),
	}
	if err := mm.SetMode(mode); err != nil {
		return nil, err
	}
	return &mm, nil
}

// SetMode updates the mode ModalManager is running in. Changing the mode is
// an expensive operation; all underlying configs must be stopped and then
// reapplied.
func (m *ModalManager) SetMode(newMode Mode) error {
	if newMode == "" {
		newMode = DefaultMode
	}

	m.mut.Lock()
	defer m.mut.Unlock()

	var (
		prevMode   = m.mode
		prevActive = m.active
	)

	if prevMode == newMode {
		return nil
	}

	// Set the active Manager based on the new mode. "distinct" means no transformations
	// need to be applied and we can use the wrapped Manager directly. Otherwise, we need
	// to create a new Manager to apply transformations.
	switch newMode {
	case ModeDistinct:
		m.active = m.wrapped
	case ModeShared:
		m.active = NewGroupManager(m.wrapped)
	default:
		panic("unknown mode " + m.mode)
	}
	m.mode = newMode

	// Remove all configs from the previous active Manager.
	if prevActive != nil {
		prevActive.Stop()
	}

	// Re-apply configs to the new active Manager.
	var firstError error
	for name, cfg := range m.configs {
		err := m.active.ApplyConfig(cfg)
		if err != nil {
			level.Error(m.log).Log("msg", "failed to apply config when changing modes", "name", name, "prev_mode", prevMode, "new_mode", newMode, "err", err)
		}
		if firstError == nil && err != nil {
			firstError = err
		}
	}

	return firstError
}

// GetInstance implements Manager.
func (m *ModalManager) GetInstance(name string) (ManagedInstance, error) {
	m.mut.RLock()
	defer m.mut.RUnlock()
	return m.active.GetInstance(name)
}

// ListInstances implements Manager.
func (m *ModalManager) ListInstances() map[string]ManagedInstance {
	m.mut.RLock()
	defer m.mut.RUnlock()
	return m.active.ListInstances()
}

// ListConfigs implements Manager.
func (m *ModalManager) ListConfigs() map[string]Config {
	m.mut.RLock()
	defer m.mut.RUnlock()
	return m.active.ListConfigs()
}

// ApplyConfig implements Manager.
func (m *ModalManager) ApplyConfig(c Config) error {
	m.mut.Lock()
	defer m.mut.Unlock()

	if err := m.active.ApplyConfig(c); err != nil {
		return err
	}

	if _, existingConfig := m.configs[c.Name]; !existingConfig {
		m.currentActiveConfigs.Inc()
		m.changedConfigs.WithLabelValues("created").Inc()
	} else {
		m.changedConfigs.WithLabelValues("updated").Inc()
	}

	m.configs[c.Name] = c

	return nil
}

// DeleteConfig implements Manager.
func (m *ModalManager) DeleteConfig(name string) error {
	m.mut.Lock()
	defer m.mut.Unlock()

	if err := m.active.DeleteConfig(name); err != nil {
		return err
	}

	if _, existingConfig := m.configs[name]; existingConfig {
		m.currentActiveConfigs.Dec()
		delete(m.configs, name)
	}

	m.changedConfigs.WithLabelValues("deleted").Inc()
	return nil
}

// Stop implements Manager.
func (m *ModalManager) Stop() {
	m.mut.Lock()
	defer m.mut.Unlock()

	m.active.Stop()
	m.currentActiveConfigs.Set(0)
	m.configs = make(map[string]Config)
}
