package querier

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	querier_worker "github.com/grafana/loki/pkg/querier/worker"
)

func Test_InitQuerierService(t *testing.T) {
	var mockQueryHandlers = map[string]http.Handler{
		"/loki/api/v1/query": http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
			_, err := res.Write([]byte(`{"handler": "test"}`))
			res.Header().Del("Content-Type")
			require.NoError(t, err)
		}),
	}

	var alwaysExternalHandlers = map[string]http.Handler{
		"/loki/api/v1/tail": http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
			_, err := res.Write([]byte("test tail handler"))
			require.NoError(t, err)
		}),
	}

	testContext := func(config WorkerServiceConfig, authMiddleware middleware.Interface) (*mux.Router, services.Service) {
		externalRouter := mux.NewRouter()

		if authMiddleware == nil {
			authMiddleware = middleware.Identity
		}

		pathPrefix := ""
		querierWorkerService, err := InitWorkerService(
			config,
			nil,
			pathPrefix,
			mockQueryHandlers,
			alwaysExternalHandlers,
			externalRouter,
			http.HandlerFunc(externalRouter.ServeHTTP),
			authMiddleware,
		)
		require.NoError(t, err)

		return externalRouter, querierWorkerService
	}

	t.Run("when querier is configured to run standalone, without a query frontend", func(t *testing.T) {
		t.Run("register the internal query handlers externally", func(t *testing.T) {
			config := WorkerServiceConfig{
				QueryFrontendEnabled:  false,
				QuerySchedulerEnabled: false,
				AllEnabled:            false,
				QuerierWorkerConfig:   &querier_worker.Config{},
			}

			externalRouter, _ := testContext(config, nil)

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest("GET", "/loki/api/v1/query", nil)
			externalRouter.ServeHTTP(recorder, request)
			assert.Equal(t, 200, recorder.Code)
			assert.Equal(t, `{"handler": "test"}`, recorder.Body.String())

			// Tail endpoints always external
			recorder = httptest.NewRecorder()
			request = httptest.NewRequest("GET", "/loki/api/v1/tail", nil)
			externalRouter.ServeHTTP(recorder, request)
			assert.Equal(t, 200, recorder.Code)
			assert.Equal(t, "test tail handler", recorder.Body.String())
		})

		t.Run("wrap external handler with auth middleware", func(t *testing.T) {
			config := WorkerServiceConfig{
				QueryFrontendEnabled:  false,
				QuerySchedulerEnabled: false,
				AllEnabled:            false,
				QuerierWorkerConfig:   &querier_worker.Config{},
			}

			requestedAuthenticated := false
			mockAuthMiddleware := middleware.Func(func(next http.Handler) http.Handler {
				requestedAuthenticated = true
				return next
			})

			externalRouter, _ := testContext(config, mockAuthMiddleware)

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest("GET", "/loki/api/v1/query", nil)
			externalRouter.ServeHTTP(recorder, request)
			assert.True(t, requestedAuthenticated)
		})

		t.Run("wrap external handler with response json middleware", func(t *testing.T) {
			// note: this test only assures that the content type of the response is
			// set if the handler function does not override it, which happens in the
			// actual implementation, see
			// https://github.com/grafana/loki/blob/34a012adcfade43291de3a7670f53679ea06aefe/pkg/lokifrontend/frontend/transport/handler.go#L136-L139
			config := WorkerServiceConfig{
				QueryFrontendEnabled:  false,
				QuerySchedulerEnabled: false,
				AllEnabled:            false,
				QuerierWorkerConfig:   &querier_worker.Config{},
			}

			externalRouter, _ := testContext(config, nil)

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest("GET", "/loki/api/v1/query", nil)
			externalRouter.ServeHTTP(recorder, request)

			contentTypeHeader := recorder.Header().Get("Content-Type")
			assert.Equal(t, "application/json; charset=UTF-8", contentTypeHeader)
		})

		t.Run("do not create a querier worker service if neither frontend address nor scheduler address has been configured", func(t *testing.T) {
			config := WorkerServiceConfig{
				QueryFrontendEnabled:  false,
				QuerySchedulerEnabled: false,
				AllEnabled:            false,
				QuerierWorkerConfig:   &querier_worker.Config{},
			}

			_, workerService := testContext(config, nil)
			assert.Nil(t, workerService)
		})

		t.Run("return a querier worker service if frontend or scheduler address has been configured", func(t *testing.T) {
			withFrontendConfig := WorkerServiceConfig{
				QuerierWorkerConfig: &querier_worker.Config{
					FrontendAddress: "http://example.com",
				},
			}
			withSchedulerConfig := WorkerServiceConfig{
				QuerierWorkerConfig: &querier_worker.Config{
					SchedulerAddress: "http://example.com",
				},
			}

			for _, config := range []WorkerServiceConfig{
				withFrontendConfig,
				withSchedulerConfig,
			} {
				_, workerService := testContext(config, nil)
				assert.NotNil(t, workerService)
			}
		})
	})

	t.Run("when query frontend, scheduler, or all target is enabled", func(t *testing.T) {
		defaultWorkerConfig := querier_worker.Config{}
		nonStandaloneTargetPermutations := []WorkerServiceConfig{
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
				externalRouter, _ := testContext(config, nil)
				recorder := httptest.NewRecorder()
				request := httptest.NewRequest("GET", "/loki/api/v1/query", nil)
				externalRouter.ServeHTTP(recorder, request)
				assert.Equal(t, 404, recorder.Code)

				// Tail endpoints always external
				recorder = httptest.NewRecorder()
				request = httptest.NewRequest("GET", "/loki/api/v1/tail", nil)
				externalRouter.ServeHTTP(recorder, request)
				assert.Equal(t, 200, recorder.Code)
				assert.Equal(t, "test tail handler", recorder.Body.String())
			}
		})

		t.Run("use localhost as the worker address if none is set", func(t *testing.T) {
			for _, config := range nonStandaloneTargetPermutations {
				workerConfig := querier_worker.Config{}
				config.QuerierWorkerConfig = &workerConfig
				config.GrpcListenPort = 1234

				testContext(config, nil)

				assert.Equal(t, "127.0.0.1:1234", workerConfig.FrontendAddress)
			}
		})

		t.Run("always return a query worker service", func(t *testing.T) {
			for _, config := range nonStandaloneTargetPermutations {
				workerConfig := querier_worker.Config{}
				config.QuerierWorkerConfig = &workerConfig
				config.GrpcListenPort = 1234

				_, querierWorkerService := testContext(config, nil)

				assert.NotNil(t, querierWorkerService)
			}
		})
	})
}
