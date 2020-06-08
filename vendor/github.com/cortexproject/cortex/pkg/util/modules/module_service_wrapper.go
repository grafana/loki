package modules

import (
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/services"
)

// This function wraps module service, and adds waiting for dependencies to start before starting,
// and dependant modules to stop before stopping this module service.
func newModuleServiceWrapper(serviceMap map[string]services.Service, mod string, modServ services.Service, startDeps []string, stopDeps []string) services.Service {
	getDeps := func(deps []string) map[string]services.Service {
		r := map[string]services.Service{}
		for _, m := range deps {
			s := serviceMap[m]
			if s != nil {
				r[m] = s
			}
		}
		return r
	}

	return util.NewModuleService(mod, modServ,
		func(_ string) map[string]services.Service {
			return getDeps(startDeps)
		},
		func(_ string) map[string]services.Service {
			return getDeps(stopDeps)
		},
	)
}
