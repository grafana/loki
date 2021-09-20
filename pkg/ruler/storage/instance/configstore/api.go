// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package configstore

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gorilla/mux"
	"github.com/grafana/agent/pkg/metrics/cluster/configapi"
	"github.com/grafana/agent/pkg/metrics/instance"
	"github.com/prometheus/client_golang/prometheus"
)

// API is an HTTP API to interact with a configstore.
type API struct {
	log       log.Logger
	storeMut  sync.Mutex
	store     Store
	validator Validator

	totalCreatedConfigs prometheus.Counter
	totalUpdatedConfigs prometheus.Counter
	totalDeletedConfigs prometheus.Counter
}

// Validator valides a config before putting it into the store.
// Validator is allowed to mutate the config and will only be given a copy.
type Validator = func(c *instance.Config) error

// NewAPI creates a new API. Store can be applied later with SetStore.
func NewAPI(l log.Logger, store Store, v Validator) *API {
	return &API{
		log:       l,
		store:     store,
		validator: v,

		totalCreatedConfigs: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "agent_prometheus_ha_configs_created_total",
			Help: "Total number of created scraping service configs",
		}),
		totalUpdatedConfigs: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "agent_prometheus_ha_configs_updated_total",
			Help: "Total number of updated scraping service configs",
		}),
		totalDeletedConfigs: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "agent_prometheus_ha_configs_deleted_total",
			Help: "Total number of deleted scraping service configs",
		}),
	}
}

// WireAPI injects routes into the provided mux router for the config
// store API.
func (api *API) WireAPI(r *mux.Router) {
	// Support URL-encoded config names. The handlers will need to decode the
	// name when reading the path variable.
	r = r.UseEncodedPath()

	r.HandleFunc("/agent/api/v1/configs", api.ListConfigurations).Methods("GET")
	r.HandleFunc("/agent/api/v1/configs/{name}", api.GetConfiguration).Methods("GET")
	r.HandleFunc("/agent/api/v1/config/{name}", api.PutConfiguration).Methods("PUT", "POST")
	r.HandleFunc("/agent/api/v1/config/{name}", api.DeleteConfiguration).Methods("DELETE")
}

// Describe implements prometheus.Collector.
func (api *API) Describe(ch chan<- *prometheus.Desc) {
	ch <- api.totalCreatedConfigs.Desc()
	ch <- api.totalUpdatedConfigs.Desc()
	ch <- api.totalDeletedConfigs.Desc()
}

// Collect implements prometheus.Collector.
func (api *API) Collect(mm chan<- prometheus.Metric) {
	mm <- api.totalCreatedConfigs
	mm <- api.totalUpdatedConfigs
	mm <- api.totalDeletedConfigs
}

// ListConfigurations returns a list of configurations.
func (api *API) ListConfigurations(rw http.ResponseWriter, r *http.Request) {
	api.storeMut.Lock()
	defer api.storeMut.Unlock()
	if api.store == nil {
		api.writeError(rw, http.StatusNotFound, fmt.Errorf("no config store running"))
		return
	}

	keys, err := api.store.List(r.Context())
	if errors.Is(err, ErrNotConnected) {
		api.writeError(rw, http.StatusNotFound, fmt.Errorf("no config store running"))
		return
	} else if err != nil {
		api.writeError(rw, http.StatusInternalServerError, fmt.Errorf("failed to write config: %w", err))
		return
	}
	api.writeResponse(rw, http.StatusOK, configapi.ListConfigurationsResponse{Configs: keys})
}

// GetConfiguration gets an individual configuration.
func (api *API) GetConfiguration(rw http.ResponseWriter, r *http.Request) {
	api.storeMut.Lock()
	defer api.storeMut.Unlock()
	if api.store == nil {
		api.writeError(rw, http.StatusNotFound, fmt.Errorf("no config store running"))
		return
	}

	configKey, err := getConfigName(r)
	if err != nil {
		api.writeError(rw, http.StatusBadRequest, err)
		return
	}

	cfg, err := api.store.Get(r.Context(), configKey)
	switch {
	case errors.Is(err, ErrNotConnected):
		api.writeError(rw, http.StatusNotFound, err)
	case errors.As(err, &NotExistError{}):
		api.writeError(rw, http.StatusBadRequest, err)
	case err != nil:
		api.writeError(rw, http.StatusInternalServerError, err)
	case err == nil:
		bb, err := instance.MarshalConfig(&cfg, false)
		if err != nil {
			api.writeError(rw, http.StatusInternalServerError, fmt.Errorf("could not marshal config for response: %w", err))
			return
		}
		api.writeResponse(rw, http.StatusOK, &configapi.GetConfigurationResponse{
			Value: string(bb),
		})
	}
}

// PutConfiguration creates or updates a configuration.
func (api *API) PutConfiguration(rw http.ResponseWriter, r *http.Request) {
	api.storeMut.Lock()
	defer api.storeMut.Unlock()
	if api.store == nil {
		api.writeError(rw, http.StatusNotFound, fmt.Errorf("no config store running"))
		return
	}

	configName, err := getConfigName(r)
	if err != nil {
		api.writeError(rw, http.StatusBadRequest, err)
		return
	}

	var config strings.Builder
	if _, err := io.Copy(&config, r.Body); err != nil {
		api.writeError(rw, http.StatusInternalServerError, err)
		return
	}

	cfg, err := instance.UnmarshalConfig(strings.NewReader(config.String()))
	if err != nil {
		api.writeError(rw, http.StatusBadRequest, fmt.Errorf("could not unmarshal config: %w", err))
		return
	}
	cfg.Name = configName

	if api.validator != nil {
		validateCfg, err := instance.UnmarshalConfig(strings.NewReader(config.String()))
		if err != nil {
			api.writeError(rw, http.StatusBadRequest, fmt.Errorf("could not unmarshal config: %w", err))
			return
		}
		validateCfg.Name = configName

		if err := api.validator(validateCfg); err != nil {
			api.writeError(rw, http.StatusBadRequest, fmt.Errorf("failed to validate config: %w", err))
			return
		}
	}

	created, err := api.store.Put(r.Context(), *cfg)
	switch {
	case errors.Is(err, ErrNotConnected):
		api.writeError(rw, http.StatusNotFound, err)
	case errors.As(err, &NotUniqueError{}):
		api.writeError(rw, http.StatusBadRequest, err)
	case err != nil:
		api.writeError(rw, http.StatusInternalServerError, err)
	default:
		if created {
			api.totalCreatedConfigs.Inc()
			api.writeResponse(rw, http.StatusCreated, nil)
		} else {
			api.totalUpdatedConfigs.Inc()
			api.writeResponse(rw, http.StatusOK, nil)
		}
	}
}

// DeleteConfiguration deletes a configuration.
func (api *API) DeleteConfiguration(rw http.ResponseWriter, r *http.Request) {
	api.storeMut.Lock()
	defer api.storeMut.Unlock()
	if api.store == nil {
		api.writeError(rw, http.StatusNotFound, fmt.Errorf("no config store running"))
		return
	}

	configKey, err := getConfigName(r)
	if err != nil {
		api.writeError(rw, http.StatusBadRequest, err)
		return
	}

	err = api.store.Delete(r.Context(), configKey)
	switch {
	case errors.Is(err, ErrNotConnected):
		api.writeError(rw, http.StatusNotFound, err)
	case errors.As(err, &NotExistError{}):
		api.writeError(rw, http.StatusBadRequest, err)
	case err != nil:
		api.writeError(rw, http.StatusInternalServerError, err)
	default:
		api.totalDeletedConfigs.Inc()
		api.writeResponse(rw, http.StatusOK, nil)
	}
}

func (api *API) writeError(rw http.ResponseWriter, statusCode int, writeErr error) {
	err := configapi.WriteError(rw, statusCode, writeErr)
	if err != nil {
		level.Error(api.log).Log("msg", "failed to write response", "err", err)
	}
}

func (api *API) writeResponse(rw http.ResponseWriter, statusCode int, v interface{}) {
	err := configapi.WriteResponse(rw, statusCode, v)
	if err != nil {
		level.Error(api.log).Log("msg", "failed to write response", "err", err)
	}
}

// getConfigName uses gorilla/mux's route variables to extract the
// "name" variable. If not found, getConfigName will panic.
func getConfigName(r *http.Request) (string, error) {
	vars := mux.Vars(r)
	name := vars["name"]
	name, err := url.PathUnescape(name)
	if err != nil {
		return "", fmt.Errorf("could not decode config name: %w", err)
	}
	return name, nil
}
