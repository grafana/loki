package modules

import (
	"fmt"
	"sort"

	"github.com/pkg/errors"

	"github.com/cortexproject/cortex/pkg/util/services"
)

// module is the basic building block of the application
type module struct {
	// dependencies of this module
	deps []string

	// initFn for this module (can return nil)
	initFn func() (services.Service, error)

	// is this module user visible (i.e intended to be passed to `InitModuleServices`)
	userVisible bool
}

// Manager is a component that initialises modules of the application
// in the right order of dependencies.
type Manager struct {
	modules map[string]*module
}

// UserInvisibleModule is an option for `RegisterModule` that marks module not visible to user. Modules are user visible by default.
func UserInvisibleModule(m *module) {
	m.userVisible = false
}

// NewManager creates a new Manager
func NewManager() *Manager {
	return &Manager{
		modules: make(map[string]*module),
	}
}

// RegisterModule registers a new module with name, init function, and options. Name must
// be unique to avoid overwriting modules. If initFn is nil, the module will not initialise.
// Modules are user visible by default.
func (m *Manager) RegisterModule(name string, initFn func() (services.Service, error), options ...func(option *module)) {
	m.modules[name] = &module{
		initFn:      initFn,
		userVisible: true,
	}

	for _, o := range options {
		o(m.modules[name])
	}
}

// AddDependency adds a dependency from name(source) to dependsOn(targets)
// An error is returned if the source module name is not found
func (m *Manager) AddDependency(name string, dependsOn ...string) error {
	if mod, ok := m.modules[name]; ok {
		mod.deps = append(mod.deps, dependsOn...)
	} else {
		return fmt.Errorf("no such module: %s", name)
	}
	return nil
}

// InitModuleServices initialises given modules by initialising all their dependencies
// in the right order. Modules are wrapped in such a way that they start after their
// dependencies have been started and stop before their dependencies are stopped.
func (m *Manager) InitModuleServices(modules ...string) (map[string]services.Service, error) {
	servicesMap := map[string]services.Service{}
	initMap := map[string]bool{}

	for _, module := range modules {
		if err := m.initModule(module, initMap, servicesMap); err != nil {
			return nil, err
		}
	}

	return servicesMap, nil
}

func (m *Manager) initModule(name string, initMap map[string]bool, servicesMap map[string]services.Service) error {
	if _, ok := m.modules[name]; !ok {
		return fmt.Errorf("unrecognised module name: %s", name)
	}

	// initialize all of our dependencies first
	deps := m.orderedDeps(name)
	deps = append(deps, name) // lastly, initialize the requested module

	for ix, n := range deps {
		// Skip already initialized modules
		if initMap[n] {
			continue
		}

		mod := m.modules[n]

		var serv services.Service

		if mod.initFn != nil {
			s, err := mod.initFn()
			if err != nil {
				return errors.Wrap(err, fmt.Sprintf("error initialising module: %s", n))
			}

			if s != nil {
				// We pass servicesMap, which isn't yet complete. By the time service starts,
				// it will be fully built, so there is no need for extra synchronization.
				serv = newModuleServiceWrapper(servicesMap, n, s, m.DependenciesForModule(n), m.findInverseDependencies(n, deps[ix+1:]))
			}
		}

		if serv != nil {
			servicesMap[n] = serv
		}

		initMap[n] = true
	}

	return nil
}

// UserVisibleModuleNames gets list of module names that are
// user visible. Returned list is sorted in increasing order.
func (m *Manager) UserVisibleModuleNames() []string {
	var result []string
	for key, val := range m.modules {
		if val.userVisible {
			result = append(result, key)
		}
	}

	sort.Strings(result)

	return result
}

// IsUserVisibleModule check if given module is public or not. Returns true
// if and only if the given module is registered and is public.
func (m *Manager) IsUserVisibleModule(mod string) bool {
	val, ok := m.modules[mod]

	if ok {
		return val.userVisible
	}

	return false
}

// IsModuleRegistered checks if the given module has been registered or not. Returns true
// if the module has previously been registered via a call to RegisterModule, false otherwise.
func (m *Manager) IsModuleRegistered(mod string) bool {
	_, ok := m.modules[mod]
	return ok
}

// listDeps recursively gets a list of dependencies for a passed moduleName
func (m *Manager) listDeps(mod string) []string {
	deps := m.modules[mod].deps
	for _, d := range m.modules[mod].deps {
		deps = append(deps, m.listDeps(d)...)
	}
	return deps
}

// orderedDeps gets a list of all dependencies ordered so that items are always after any of their dependencies.
func (m *Manager) orderedDeps(mod string) []string {
	deps := m.listDeps(mod)

	// get a unique list of moduleNames, with a flag for whether they have been added to our result
	uniq := map[string]bool{}
	for _, dep := range deps {
		uniq[dep] = false
	}

	result := make([]string, 0, len(uniq))

	// keep looping through all modules until they have all been added to the result.

	for len(result) < len(uniq) {
	OUTER:
		for name, added := range uniq {
			if added {
				continue
			}
			for _, dep := range m.modules[name].deps {
				// stop processing this module if one of its dependencies has
				// not been added to the result yet.
				if !uniq[dep] {
					continue OUTER
				}
			}

			// if all of the module's dependencies have been added to the result slice,
			// then we can safely add this module to the result slice as well.
			uniq[name] = true
			result = append(result, name)
		}
	}
	return result
}

// find modules in the supplied list, that depend on mod
func (m *Manager) findInverseDependencies(mod string, mods []string) []string {
	result := []string(nil)

	for _, n := range mods {
		for _, d := range m.modules[n].deps {
			if d == mod {
				result = append(result, n)
				break
			}
		}
	}

	return result
}

// DependenciesForModule returns transitive dependencies for given module, sorted by name.
func (m *Manager) DependenciesForModule(module string) []string {
	dedup := map[string]bool{}
	for _, d := range m.listDeps(module) {
		dedup[d] = true
	}

	result := make([]string, 0, len(dedup))
	for d := range dedup {
		result = append(result, d)
	}
	sort.Strings(result)
	return result
}
