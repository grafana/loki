package loki

import (
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/services"
)

// This function wraps module service, and adds waiting for dependencies to start before starting,
// and dependant modules to stop before stopping this module service.
func newModuleServiceWrapper(serviceMap map[moduleName]services.Service, mod moduleName, modServ services.Service, startDeps []moduleName, stopDeps []moduleName) services.Service {
	getDeps := func(deps []moduleName) map[string]services.Service {
		r := map[string]services.Service{}
		for _, m := range deps {
			s := serviceMap[m]
			if s != nil {
				r[string(m)] = s
			}
		}
		return r
	}

	return util.NewModuleService(string(mod), modServ,
		func(_ string) map[string]services.Service {
			return getDeps(startDeps)
		},
		func(_ string) map[string]services.Service {
			return getDeps(stopDeps)
		},
	)
}
