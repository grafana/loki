// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package configstore

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/gorilla/mux"
	"github.com/grafana/agent/pkg/client"
	"github.com/grafana/agent/pkg/metrics/cluster/configapi"
	"github.com/grafana/agent/pkg/metrics/instance"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPI_ListConfigurations(t *testing.T) {
	s := &Mock{
		ListFunc: func(ctx context.Context) ([]string, error) {
			return []string{"a", "b", "c"}, nil
		},
	}

	api := NewAPI(log.NewNopLogger(), s, nil)
	env := newAPITestEnvironment(t, api)

	resp, err := http.Get(env.srv.URL + "/agent/api/v1/configs")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	expect := `{
		"status": "success",
		"data": {
			"configs": ["a", "b", "c"]
		}
	}`
	body, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	require.JSONEq(t, expect, string(body))

	t.Run("With Client", func(t *testing.T) {
		cli := client.New(env.srv.URL)
		apiResp, err := cli.ListConfigs(context.Background())
		require.NoError(t, err)

		expect := &configapi.ListConfigurationsResponse{Configs: []string{"a", "b", "c"}}
		require.Equal(t, expect, apiResp)
	})
}

func TestAPI_GetConfiguration_Invalid(t *testing.T) {
	s := &Mock{
		GetFunc: func(ctx context.Context, key string) (instance.Config, error) {
			return instance.Config{}, NotExistError{Key: key}
		},
	}

	api := NewAPI(log.NewNopLogger(), s, nil)
	env := newAPITestEnvironment(t, api)

	resp, err := http.Get(env.srv.URL + "/agent/api/v1/configs/does-not-exist")
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	expect := `{
		"status": "error",
		"data": {
			"error": "configuration does-not-exist does not exist"
		}
	}`
	body, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	require.JSONEq(t, expect, string(body))

	t.Run("With Client", func(t *testing.T) {
		cli := client.New(env.srv.URL)
		_, err := cli.GetConfiguration(context.Background(), "does-not-exist")
		require.NotNil(t, err)
		require.Equal(t, "configuration does-not-exist does not exist", err.Error())
	})
}

func TestAPI_GetConfiguration(t *testing.T) {
	s := &Mock{
		GetFunc: func(ctx context.Context, key string) (instance.Config, error) {
			return instance.Config{
				Name:                key,
				HostFilter:          true,
				RemoteFlushDeadline: 10 * time.Minute,
			}, nil
		},
	}

	api := NewAPI(log.NewNopLogger(), s, nil)
	env := newAPITestEnvironment(t, api)

	resp, err := http.Get(env.srv.URL + "/agent/api/v1/configs/exists")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	expect := `{
		"status": "success",
		"data": {
			"value": "name: exists\nhost_filter: true\nremote_flush_deadline: 10m0s\n"
		}
	}`
	body, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	require.JSONEq(t, expect, string(body))

	t.Run("With Client", func(t *testing.T) {
		cli := client.New(env.srv.URL)
		actual, err := cli.GetConfiguration(context.Background(), "exists")
		require.NoError(t, err)

		// The client will apply defaults, so we need to start with the DefaultConfig
		// as a base here.
		expect := instance.DefaultConfig
		expect.Name = "exists"
		expect.HostFilter = true
		expect.RemoteFlushDeadline = 10 * time.Minute
		require.Equal(t, &expect, actual)
	})
}

func TestServer_PutConfiguration(t *testing.T) {
	var s Mock

	api := NewAPI(log.NewNopLogger(), &s, nil)
	env := newAPITestEnvironment(t, api)

	cfg := instance.Config{Name: "newconfig"}
	bb, err := instance.MarshalConfig(&cfg, false)
	require.NoError(t, err)

	t.Run("Created", func(t *testing.T) {
		// Created configs should return http.StatusCreated
		s.PutFunc = func(ctx context.Context, c instance.Config) (created bool, err error) {
			return true, nil
		}

		resp, err := http.Post(env.srv.URL+"/agent/api/v1/config/newconfig", "", bytes.NewReader(bb))
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, resp.StatusCode)
	})

	t.Run("Updated", func(t *testing.T) {
		// Updated configs should return http.StatusOK
		s.PutFunc = func(ctx context.Context, c instance.Config) (created bool, err error) {
			return false, nil
		}

		resp, err := http.Post(env.srv.URL+"/agent/api/v1/config/newconfig", "", bytes.NewReader(bb))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestServer_PutConfiguration_Invalid(t *testing.T) {
	var s Mock

	api := NewAPI(log.NewNopLogger(), &s, func(c *instance.Config) error {
		return fmt.Errorf("custom validation error")
	})
	env := newAPITestEnvironment(t, api)

	cfg := instance.Config{Name: "newconfig"}
	bb, err := instance.MarshalConfig(&cfg, false)
	require.NoError(t, err)

	resp, err := http.Post(env.srv.URL+"/agent/api/v1/config/newconfig", "", bytes.NewReader(bb))
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	expect := `{
		"status": "error",
		"data": {
			"error": "failed to validate config: custom validation error"
		}
	}`
	body, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	require.JSONEq(t, expect, string(body))
}

func TestServer_PutConfiguration_WithClient(t *testing.T) {
	var s Mock
	api := NewAPI(log.NewNopLogger(), &s, nil)
	env := newAPITestEnvironment(t, api)

	cfg := instance.DefaultConfig
	cfg.Name = "newconfig-withclient"
	cfg.HostFilter = true
	cfg.RemoteFlushDeadline = 10 * time.Minute

	s.PutFunc = func(ctx context.Context, c instance.Config) (created bool, err error) {
		assert.Equal(t, cfg, c)
		return true, nil
	}

	cli := client.New(env.srv.URL)
	err := cli.PutConfiguration(context.Background(), "newconfig-withclient", &cfg)
	require.NoError(t, err)
}

func TestServer_DeleteConfiguration(t *testing.T) {
	s := &Mock{
		DeleteFunc: func(ctx context.Context, key string) error {
			assert.Equal(t, "deleteme", key)
			return nil
		},
	}

	api := NewAPI(log.NewNopLogger(), s, nil)
	env := newAPITestEnvironment(t, api)

	req, err := http.NewRequest(http.MethodDelete, env.srv.URL+"/agent/api/v1/config/deleteme", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	t.Run("With Client", func(t *testing.T) {
		cli := client.New(env.srv.URL)
		err := cli.DeleteConfiguration(context.Background(), "deleteme")
		require.NoError(t, err)
	})
}

func TestServer_URLEncoded(t *testing.T) {
	var s Mock

	api := NewAPI(log.NewNopLogger(), &s, nil)
	env := newAPITestEnvironment(t, api)

	var cfg instance.Config
	bb, err := instance.MarshalConfig(&cfg, false)
	require.NoError(t, err)

	s.PutFunc = func(ctx context.Context, c instance.Config) (created bool, err error) {
		assert.Equal(t, "url/encoded", c.Name)
		return true, nil
	}

	resp, err := http.Post(env.srv.URL+"/agent/api/v1/config/url%2Fencoded", "", bytes.NewReader(bb))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	s.GetFunc = func(ctx context.Context, key string) (instance.Config, error) {
		assert.Equal(t, "url/encoded", key)
		return instance.Config{Name: "url/encoded"}, nil
	}

	resp, err = http.Get(env.srv.URL + "/agent/api/v1/configs/url%2Fencoded")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

type apiTestEnvironment struct {
	srv    *httptest.Server
	router *mux.Router
}

func newAPITestEnvironment(t *testing.T, api *API) apiTestEnvironment {
	t.Helper()

	router := mux.NewRouter()
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	api.WireAPI(router)

	return apiTestEnvironment{srv: srv, router: router}
}
