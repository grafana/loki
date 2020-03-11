package loki

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/services"
)

// This service wraps module service, and adds waiting for dependencies to start before starting,
// and dependant modules to stop before stopping this module service.
type moduleServiceWrapper struct {
	services.Service

	serviceMap map[moduleName]services.Service
	module     moduleName
	service    services.Service
	startDeps  []moduleName
	stopDeps   []moduleName
}

func newModuleServiceWrapper(serviceMap map[moduleName]services.Service, mod moduleName, modServ services.Service, startDeps []moduleName, stopDeps []moduleName) *moduleServiceWrapper {
	w := &moduleServiceWrapper{
		serviceMap: serviceMap,
		module:     mod,
		service:    modServ,
		startDeps:  startDeps,
		stopDeps:   stopDeps,
	}

	w.Service = services.NewBasicService(w.start, w.run, w.stop)
	return w
}

func (w *moduleServiceWrapper) start(serviceContext context.Context) error {
	// wait until all startDeps are running
	for _, m := range w.startDeps {
		s := w.serviceMap[m]
		if s == nil {
			continue
		}

		level.Debug(util.Logger).Log("msg", "module waiting for initialization", "module", w.module, "waiting_for", m)

		err := s.AwaitRunning(serviceContext)
		if err != nil {
			return fmt.Errorf("failed to start %v, because it depends on module %v, which has failed: %w", w.module, m, err)
		}
	}

	// we don't want to let service to stop until we stop all dependant services,
	// so we use new context
	level.Info(util.Logger).Log("msg", "initialising", "module", w.module)
	err := w.service.StartAsync(context.Background())
	if err != nil {
		return errors.Wrapf(err, "error starting module: %s", w.module)
	}

	return w.service.AwaitRunning(serviceContext)
}

func (w *moduleServiceWrapper) run(serviceContext context.Context) error {
	// wait until service stops, or context is canceled, whatever happens first.
	// We don't care about exact error here
	_ = w.service.AwaitTerminated(serviceContext)
	return w.service.FailureCase()
}

func (w *moduleServiceWrapper) stop(_ error) error {
	// wait until all stopDeps have stopped
	for _, m := range w.stopDeps {
		s := w.serviceMap[m]
		if s == nil {
			continue
		}

		// Passed context isn't canceled, so we can only get error here, if service
		// fails. But we don't care *how* service stops, as long as it is done.
		_ = s.AwaitTerminated(context.Background())
	}

	level.Debug(util.Logger).Log("msg", "stopping", "module", w.module)

	err := services.StopAndAwaitTerminated(context.Background(), w.service)
	if err != nil {
		level.Warn(util.Logger).Log("msg", "error stopping module", "module", w.module, "err", err)
	} else {
		level.Info(util.Logger).Log("msg", "module stopped", "module", w.module)
	}
	return err
}
