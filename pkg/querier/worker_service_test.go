package querier

import (
	"net/http"
	"net/http/httptest"
	"testing"

	querier_worker "github.com/cortexproject/cortex/pkg/querier/worker"
	"github.com/grafana/dskit/services"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/middleware"
)

func Test_InitQuerierService(t *testing.T) {
	requestedAuthenticated := false
	noopWrapper := middleware.Func(func(next http.Handler) http.Handler {
		requestedAuthenticated = true
		return next
	})

	var mockQueryHandlers = map[string]http.Handler{
		"/loki/api/v1/query": http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
			res.Write([]byte("test handler"))
		}),
	}

	testContext := func(config QuerierWorkerServiceConfig) (*mux.Router, services.Service) {
		requestedAuthenticated = false
		externalRouter := mux.NewRouter()
		querierWorkerService, err := InitQuerierWorkerService(config, mockQueryHandlers, externalRouter, http.HandlerFunc(externalRouter.ServeHTTP), noopWrapper)
		require.NoError(t, err)

		return externalRouter, querierWorkerService
	}

	t.Run("when querier is configured to run standalone, without a query frontend", func(t *testing.T) {
		t.Run("register the internal query handlers externally", func(t *testing.T) {
			config := QuerierWorkerServiceConfig{
				QueryFrontendEnabled:  false,
				QuerySchedulerEnabled: false,
				AllEnabled:            false,
				QuerierWorkerConfig:   &querier_worker.Config{},
			}

			externalRouter, _ := testContext(config)

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest("GET", "/loki/api/v1/query", nil)
			externalRouter.ServeHTTP(recorder, request)
			assert.Equal(t, 200, recorder.Code)
			assert.Equal(t, "test handler", recorder.Body.String())
		})

		t.Run("wrap external handler with auth middleware", func(t *testing.T) {
			config := QuerierWorkerServiceConfig{
				QueryFrontendEnabled:  false,
				QuerySchedulerEnabled: false,
				AllEnabled:            false,
				QuerierWorkerConfig:   &querier_worker.Config{},
			}

			externalRouter, _ := testContext(config)

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest("GET", "/loki/api/v1/query", nil)
			externalRouter.ServeHTTP(recorder, request)
			assert.True(t, requestedAuthenticated)
		})

		t.Run("wrap external handler with response json middleware", func(t *testing.T) {
			config := QuerierWorkerServiceConfig{
				QueryFrontendEnabled:  false,
				QuerySchedulerEnabled: false,
				AllEnabled:            false,
				QuerierWorkerConfig:   &querier_worker.Config{},
			}

			externalRouter, _ := testContext(config)

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest("GET", "/loki/api/v1/query", nil)
			externalRouter.ServeHTTP(recorder, request)

      contentTypeHeader := recorder.Header().Get("Content-Type")
      assert.Equal(t, "application/json; charset=UTF-8", contentTypeHeader)
		})

		t.Run("do not create a querier worker service if neither frontend address nor scheduler address has been configured", func(t *testing.T) {
			config := QuerierWorkerServiceConfig{
				QueryFrontendEnabled:  false,
				QuerySchedulerEnabled: false,
				AllEnabled:            false,
				QuerierWorkerConfig:   &querier_worker.Config{},
			}

			_, workerService := testContext(config)
			assert.Nil(t, workerService)
		})

		t.Run("return a querier worker service if frontend or scheduler address has been configured", func(t *testing.T) {
			withFrontendConfig := QuerierWorkerServiceConfig{
				QuerierWorkerConfig: &querier_worker.Config{
					FrontendAddress: "http://example.com",
				},
			}
			withSchedulerConfig := QuerierWorkerServiceConfig{
				QuerierWorkerConfig: &querier_worker.Config{
					SchedulerAddress: "http://example.com",
				},
			}

			for _, config := range []QuerierWorkerServiceConfig{
				withFrontendConfig,
				withSchedulerConfig,
			} {
				_, workerService := testContext(config)
				assert.NotNil(t, workerService)
			}
		})
	})

	t.Run("when query frontend, scheduler, or all target is enabled", func(t *testing.T) {
		defaultWorkerConfig := querier_worker.Config{}
		nonStandaloneTargetPermutations := []QuerierWorkerServiceConfig{
			{
				QueryFrontendEnabled:  true,
				QuerySchedulerEnabled: false,
				AllEnabled:            false,
				QuerierWorkerConfig:   &defaultWorkerConfig,
			},
			{
				QueryFrontendEnabled:  false,
				QuerySchedulerEnabled: true,
				AllEnabled:            false,
				QuerierWorkerConfig:   &defaultWorkerConfig,
			},
			{
				QueryFrontendEnabled:  false,
				QuerySchedulerEnabled: false,
				AllEnabled:            true,
				QuerierWorkerConfig:   &defaultWorkerConfig,
			},
			{
				QueryFrontendEnabled:  true,
				QuerySchedulerEnabled: true,
				AllEnabled:            false,
				QuerierWorkerConfig:   &defaultWorkerConfig,
			},
			{
				QueryFrontendEnabled:  true,
				QuerySchedulerEnabled: false,
				AllEnabled:            true,
				QuerierWorkerConfig:   &defaultWorkerConfig,
			},
			{
				QueryFrontendEnabled:  false,
				QuerySchedulerEnabled: true,
				AllEnabled:            true,
				QuerierWorkerConfig:   &defaultWorkerConfig,
			},
			{
				QueryFrontendEnabled:  true,
				QuerySchedulerEnabled: true,
				AllEnabled:            true,
				QuerierWorkerConfig:   &defaultWorkerConfig,
			},
		}

		t.Run("do not register the internal query handler externally", func(t *testing.T) {
			for _, config := range nonStandaloneTargetPermutations {
				externalRouter, _ := testContext(config)
				recorder := httptest.NewRecorder()
				request := httptest.NewRequest("GET", "/loki/api/v1/query", nil)
				externalRouter.ServeHTTP(recorder, request)
				assert.Equal(t, 404, recorder.Code)
			}
		})

		t.Run("use localhost as the worker address if none is set", func(t *testing.T) {
			for _, config := range nonStandaloneTargetPermutations {
				workerConfig := querier_worker.Config{}
				config.QuerierWorkerConfig = &workerConfig
				config.GrpcListenPort = 1234

				testContext(config)

				assert.Equal(t, "127.0.0.1:1234", workerConfig.FrontendAddress)
			}
		})

		t.Run("set the worker's max concurrent request to the same as the max concurrent setting for the querier", func(t *testing.T) {
			for _, config := range nonStandaloneTargetPermutations {
				workerConfig := querier_worker.Config{}
				config.QuerierWorkerConfig = &workerConfig
				config.QuerierMaxConcurrent = 42

				testContext(config)

				assert.Equal(t, 42, workerConfig.MaxConcurrentRequests)
			}
		})

		t.Run("always return a query worker service", func(t *testing.T) {
			for _, config := range nonStandaloneTargetPermutations {
				workerConfig := querier_worker.Config{}
				config.QuerierWorkerConfig = &workerConfig
				config.GrpcListenPort = 1234

				_, querierWorkerService := testContext(config)

				assert.NotNil(t, querierWorkerService)
			}
		})
	})
}
