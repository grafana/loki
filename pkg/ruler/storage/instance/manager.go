// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package instance

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/prometheus/storage"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var (
	// DefaultBasicManagerConfig is the default config for the BasicManager.
	DefaultBasicManagerConfig = BasicManagerConfig{
		InstanceRestartBackoff: 5 * time.Second,
	}
)

// Manager represents a set of methods for manipulating running instances at
// runtime.
type Manager interface {
	// GetInstance retrieves a ManagedInstance by name.
	GetInstance(name string) (ManagedInstance, error)

	// ListInstances returns all currently managed instances running
	// within the Manager. The key will be the instance name from their config.
	ListInstances() map[string]ManagedInstance

	// ListConfigs returns the config objects associated with a managed
	// instance. The key will be the Name field from Config.
	ListConfigs() map[string]Config

	// ApplyConfig creates a new Config or updates an existing Config if
	// one with Config.Name already exists.
	ApplyConfig(Config) error

	// DeleteConfig deletes a given managed instance based on its Config.Name.
	DeleteConfig(name string) error

	// Ready indicates if all instances are ready for processing.
	Ready() bool

	// InstanceReady indicates if an instance is ready for processing.
	InstanceReady(name string) bool

	// Stop stops the Manager and all managed instances.
	Stop()
}

// ManagedInstance is implemented by Instance. It is defined as an interface
// for the sake of testing from Manager implementations.
type ManagedInstance interface {
	Ready() bool
	Run(ctx context.Context) error
	Update(c Config) error
	StorageDirectory() string
	Appender(ctx context.Context) storage.Appender
	Stop() error
	Tenant() string
}

// BasicManagerConfig controls the operations of a BasicManager.
type BasicManagerConfig struct {
	InstanceRestartBackoff time.Duration
}

// BasicManager creates a new BasicManager, implementing the Manager interface.
// BasicManager will directly launch instances and perform no extra processing.
//
// Other implementations of Manager usually wrap a BasicManager.
type BasicManager struct {
	cfgMut  sync.Mutex
	cfg     BasicManagerConfig
	logger  log.Logger
	metrics *Metrics

	// Take care when locking mut: if you hold onto a lock of mut while calling
	// Stop on a process, you will deadlock.
	mut       sync.Mutex
	processes map[string]*managedProcess

	launch Factory
}

// managedProcess represents a goroutine running a ManagedInstance. cancel
// requests that the goroutine should shutdown. done will be closed after the
// goroutine exists.
type managedProcess struct {
	cfg    Config
	inst   ManagedInstance
	cancel context.CancelFunc
	done   chan bool
}

func (p managedProcess) Stop() {
	if p.inst.Ready() { // Only stop initialized instances to avoid panic
		if err := p.inst.Stop(); err != nil {
			level.Error(util_log.Logger).Log("msg", "error while stopping instance", "user", p.inst.Tenant(), "err", err)
		}
	}

	p.cancel()
	<-p.done
}

// Factory should return an unstarted instance given some config.
type Factory func(c Config) (ManagedInstance, error)

// NewBasicManager creates a new BasicManager. The launch function will be
// invoked any time a new Config is applied.
//
// The lifecycle of any ManagedInstance returned by the launch function will
// be handled by the BasicManager. Instances will be automatically restarted
// if stopped, updated if the config changes, or removed when the Config is
// deleted.
func NewBasicManager(cfg BasicManagerConfig, metrics *Metrics, logger log.Logger, launch Factory) *BasicManager {
	return &BasicManager{
		cfg:       cfg,
		metrics:   metrics,
		logger:    logger,
		processes: make(map[string]*managedProcess),
		launch:    launch,
	}
}

// UpdateManagerConfig updates the BasicManagerConfig.
func (m *BasicManager) UpdateManagerConfig(c BasicManagerConfig) {
	m.cfgMut.Lock()
	defer m.cfgMut.Unlock()
	m.cfg = c
}

// GetInstance returns the given instance by name.
func (m *BasicManager) GetInstance(name string) (ManagedInstance, error) {
	m.mut.Lock()
	defer m.mut.Unlock()

	process, ok := m.processes[name]
	if !ok {
		return nil, fmt.Errorf("instance %s does not exist", name)
	}
	return process.inst, nil
}

// ListInstances returns the current active instances managed by BasicManager.
func (m *BasicManager) ListInstances() map[string]ManagedInstance {
	m.mut.Lock()
	defer m.mut.Unlock()

	res := make(map[string]ManagedInstance, len(m.processes))
	for name, process := range m.processes {
		res[name] = process.inst
	}
	return res
}

// ListConfigs lists the current active configs managed by BasicManager.
func (m *BasicManager) ListConfigs() map[string]Config {
	m.mut.Lock()
	defer m.mut.Unlock()

	res := make(map[string]Config, len(m.processes))
	for name, process := range m.processes {
		res[name] = process.cfg
	}
	return res
}

// ApplyConfig takes a Config and either starts a new managed instance or
// updates an existing managed instance. The value for Name in c is used to
// uniquely identify the Config and determine whether the Config has an
// existing associated managed instance.
func (m *BasicManager) ApplyConfig(c Config) error {
	m.mut.Lock()
	defer m.mut.Unlock()

	// If the config already exists, we need to update it.
	proc, ok := m.processes[c.Name]
	if ok {
		err := proc.inst.Update(c)

		// If the instance could not be dynamically updated, we need to force the
		// update by restarting it. If it failed for another reason, something
		// serious went wrong and we'll completely give up without stopping the
		// existing job.
		if errors.Is(err, ErrInvalidUpdate{}) {
			level.Info(m.logger).Log("msg", "could not dynamically update instance, will manually restart", "instance", c.Name, "reason", err)

			// NOTE: we don't return here; we fall through to spawn the new instance.
			proc.Stop()
		} else if err != nil {
			return fmt.Errorf("failed to update instance %s: %w", c.Name, err)
		} else {
			level.Info(m.logger).Log("msg", "dynamically updated instance", "instance", c.Name)

			proc.cfg = c
			return nil
		}
	}

	// Spawn a new process for the new config.
	err := m.spawnProcess(c)
	if err != nil {
		return err
	}

	m.metrics.RunningInstances.Inc()
	return nil
}

func (m *BasicManager) spawnProcess(c Config) error {
	inst, err := m.launch(c)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool)

	proc := &managedProcess{
		cancel: cancel,
		done:   done,
		cfg:    c,
		inst:   inst,
	}
	m.processes[c.Name] = proc

	go func() {
		m.runProcess(ctx, c.Name, inst)
		close(done)

		// Now that the process has stopped, we can remove it from our managed
		// list.
		//
		// However, it's possible that a new Config may have been applied and
		// overwrote the initial value in our map. We only want to delete the
		// process from the map if it hasn't changed from what we initially
		// set it to.
		//
		// We only use the instance for comparing (which will never change) because
		// the instance may have dynamically been given a new config since this
		// goroutine started.
		m.mut.Lock()
		if storedProc, exist := m.processes[c.Name]; exist && storedProc.inst == inst {
			delete(m.processes, c.Name)
		}
		m.mut.Unlock()

		m.metrics.RunningInstances.Dec()
	}()

	return nil
}

// runProcess runs and instance and keeps it alive until it is explicitly stopped
// by cancelling the context.
func (m *BasicManager) runProcess(ctx context.Context, name string, inst ManagedInstance) {
	for {
		err := inst.Run(ctx)
		if err != nil && err != context.Canceled {
			backoff := m.instanceRestartBackoff()

			m.metrics.AbnormalExits.WithLabelValues(name).Inc()
			level.Error(m.logger).Log("msg", "instance stopped abnormally, restarting after backoff period", "err", err, "backoff", backoff, "instance", name)
			time.Sleep(backoff)
		} else {
			level.Info(m.logger).Log("msg", "stopped instance", "instance", name)
			break
		}
	}
}

func (m *BasicManager) instanceRestartBackoff() time.Duration {
	m.cfgMut.Lock()
	defer m.cfgMut.Unlock()
	return m.cfg.InstanceRestartBackoff
}

// DeleteConfig removes a managed instance by its config name. Returns an error
// if there is no such managed instance with the given name.
func (m *BasicManager) DeleteConfig(name string) error {
	m.mut.Lock()
	proc, ok := m.processes[name]
	if !ok {
		m.mut.Unlock()
		return errors.New("config does not exist")
	}
	m.mut.Unlock()

	// spawnProcess is responsible for removing the process from the map after it
	// stops so we don't need to delete anything from m.processes here.
	proc.Stop()
	return nil
}

// Ready indicates if all instances are ready for processing.
func (m *BasicManager) Ready() bool {
	m.mut.Lock()
	defer m.mut.Unlock()

	for _, process := range m.processes {
		if process.inst == nil {
			return false
		}

		if !process.inst.Ready() {
			return false
		}
	}

	return true
}

// InstanceReady indicates if an instance is ready for processing.
func (m *BasicManager) InstanceReady(name string) bool {
	inst, err := m.GetInstance(name)
	if err != nil {
		return false
	}

	return inst.Ready()
}

// Stop stops the BasicManager and stops all active processes for configs.
func (m *BasicManager) Stop() {
	var wg sync.WaitGroup

	// We don't need to change m.processes here; processes remove themselves
	// from the map (in spawnProcess).
	m.mut.Lock()
	wg.Add(len(m.processes))
	for _, proc := range m.processes {
		go func(proc *managedProcess) {
			proc.Stop()
			wg.Done()
		}(proc)
	}
	m.mut.Unlock()

	wg.Wait()
}

// MockManager exposes methods of the Manager interface as struct fields.
// Useful for tests.
type MockManager struct {
	GetInstanceFunc   func(name string) (ManagedInstance, error)
	ListInstancesFunc func() map[string]ManagedInstance
	ListConfigsFunc   func() map[string]Config
	ApplyConfigFunc   func(Config) error
	DeleteConfigFunc  func(name string) error
	StopFunc          func()
}

func (m MockManager) Ready() bool {
	return true
}

func (m MockManager) InstanceReady(_ string) bool {
	return true
}

// GetInstance implements Manager.
func (m MockManager) GetInstance(name string) (ManagedInstance, error) {
	if m.GetInstanceFunc != nil {
		return m.GetInstanceFunc(name)
	}
	panic("GetInstanceFunc not implemented")
}

// ListInstances implements Manager.
func (m MockManager) ListInstances() map[string]ManagedInstance {
	if m.ListInstancesFunc != nil {
		return m.ListInstancesFunc()
	}
	panic("ListInstancesFunc not implemented")
}

// ListConfigs implements Manager.
func (m MockManager) ListConfigs() map[string]Config {
	if m.ListConfigsFunc != nil {
		return m.ListConfigsFunc()
	}
	panic("ListConfigsFunc not implemented")
}

// ApplyConfig implements Manager.
func (m MockManager) ApplyConfig(c Config) error {
	if m.ApplyConfigFunc != nil {
		return m.ApplyConfigFunc(c)
	}
	panic("ApplyConfigFunc not implemented")
}

// DeleteConfig implements Manager.
func (m MockManager) DeleteConfig(name string) error {
	if m.DeleteConfigFunc != nil {
		return m.DeleteConfigFunc(name)
	}
	panic("DeleteConfigFunc not implemented")
}

// Stop implements Manager.
func (m MockManager) Stop() {
	if m.StopFunc != nil {
		m.StopFunc()
		return
	}
	panic("StopFunc not implemented")
}
