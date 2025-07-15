// Copyright 2015 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package consulagent

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/targetgroup"
)

//nolint:interfacer // this follows the pattern in prometheus service discovery
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// TODO: Add ability to unregister metrics?
func NewTestMetrics(t *testing.T, conf discovery.Config, reg prometheus.Registerer) discovery.DiscovererMetrics {
	refreshMetrics := discovery.NewRefreshMetrics(reg)
	require.NoError(t, refreshMetrics.Register())

	metrics := conf.NewDiscovererMetrics(prometheus.NewRegistry(), refreshMetrics)
	require.NoError(t, metrics.Register())

	return metrics
}

func TestConfiguredService(t *testing.T) {
	conf := &SDConfig{
		Services: []string{"configuredServiceName"}}

	metrics := NewTestMetrics(t, conf, prometheus.NewRegistry())

	consulDiscovery, err := NewDiscovery(conf, nil, metrics)

	if err != nil {
		t.Errorf("Unexpected error when initializing discovery %v", err)
	}
	if !consulDiscovery.shouldWatch("configuredServiceName", []string{""}) {
		t.Errorf("Expected service %s to be watched", "configuredServiceName")
	}
	if consulDiscovery.shouldWatch("nonConfiguredServiceName", []string{""}) {
		t.Errorf("Expected service %s to not be watched", "nonConfiguredServiceName")
	}
}

func TestConfiguredServiceWithTag(t *testing.T) {
	conf := &SDConfig{
		Services:    []string{"configuredServiceName"},
		ServiceTags: []string{"http"},
	}

	metrics := NewTestMetrics(t, conf, prometheus.NewRegistry())

	consulDiscovery, err := NewDiscovery(conf, nil, metrics)

	if err != nil {
		t.Errorf("Unexpected error when initializing discovery %v", err)
	}
	if consulDiscovery.shouldWatch("configuredServiceName", []string{""}) {
		t.Errorf("Expected service %s to not be watched without tag", "configuredServiceName")
	}
	if !consulDiscovery.shouldWatch("configuredServiceName", []string{"http"}) {
		t.Errorf("Expected service %s to be watched with tag %s", "configuredServiceName", "http")
	}
	if consulDiscovery.shouldWatch("nonConfiguredServiceName", []string{""}) {
		t.Errorf("Expected service %s to not be watched without tag", "nonConfiguredServiceName")
	}
	if consulDiscovery.shouldWatch("nonConfiguredServiceName", []string{"http"}) {
		t.Errorf("Expected service %s to not be watched with tag %s", "nonConfiguredServiceName", "http")
	}
}

func TestConfiguredServiceWithTags(t *testing.T) {
	type testcase struct {
		// What we've configured to watch.
		conf *SDConfig
		// The service we're checking if we should watch or not.
		serviceName string
		serviceTags []string
		shouldWatch bool
	}

	cases := []testcase{
		{
			conf: &SDConfig{
				Services:    []string{"configuredServiceName"},
				ServiceTags: []string{"http", "v1"},
			},
			serviceName: "configuredServiceName",
			serviceTags: []string{""},
			shouldWatch: false,
		},
		{
			conf: &SDConfig{
				Services:    []string{"configuredServiceName"},
				ServiceTags: []string{"http", "v1"},
			},
			serviceName: "configuredServiceName",
			serviceTags: []string{"http", "v1"},
			shouldWatch: true,
		},
		{
			conf: &SDConfig{
				Services:    []string{"configuredServiceName"},
				ServiceTags: []string{"http", "v1"},
			},
			serviceName: "nonConfiguredServiceName",
			serviceTags: []string{""},
			shouldWatch: false,
		},
		{
			conf: &SDConfig{
				Services:    []string{"configuredServiceName"},
				ServiceTags: []string{"http", "v1"},
			},
			serviceName: "nonConfiguredServiceName",
			serviceTags: []string{"http, v1"},
			shouldWatch: false,
		},
		{
			conf: &SDConfig{
				Services:    []string{"configuredServiceName"},
				ServiceTags: []string{"http", "v1"},
			},
			serviceName: "configuredServiceName",
			serviceTags: []string{"http", "v1", "foo"},
			shouldWatch: true,
		},
		{
			conf: &SDConfig{
				Services:    []string{"configuredServiceName"},
				ServiceTags: []string{"http", "v1", "foo"},
			},
			serviceName: "configuredServiceName",
			serviceTags: []string{"http", "v1", "foo"},
			shouldWatch: true,
		},
		{
			conf: &SDConfig{
				Services:    []string{"configuredServiceName"},
				ServiceTags: []string{"http", "v1"},
			},
			serviceName: "configuredServiceName",
			serviceTags: []string{"http", "v1", "v1"},
			shouldWatch: true,
		},
	}

	for _, tc := range cases {
		metrics := NewTestMetrics(t, tc.conf, prometheus.NewRegistry())

		consulDiscovery, err := NewDiscovery(tc.conf, nil, metrics)

		if err != nil {
			t.Errorf("Unexpected error when initializing discovery %v", err)
		}
		ret := consulDiscovery.shouldWatch(tc.serviceName, tc.serviceTags)
		if ret != tc.shouldWatch {
			t.Errorf("Expected should watch? %t, got %t. Watched service and tags: %s %+v, input was %s %+v", tc.shouldWatch, ret, tc.conf.Services, tc.conf.ServiceTags, tc.serviceName, tc.serviceTags)
		}

	}
}

func TestNonConfiguredService(t *testing.T) {
	conf := &SDConfig{}

	metrics := NewTestMetrics(t, conf, prometheus.NewRegistry())

	consulDiscovery, err := NewDiscovery(conf, nil, metrics)

	if err != nil {
		t.Errorf("Unexpected error when initializing discovery %v", err)
	}
	if !consulDiscovery.shouldWatch("nonConfiguredServiceName", []string{""}) {
		t.Errorf("Expected service %s to be watched", "nonConfiguredServiceName")
	}
}

const (
	AgentAnswer = `{
  "Config": {
    "Datacenter": "test-dc",
    "NodeName": "test-node",
    "NodeID": "efd2573b-4c48-312b-2097-99bffc4352c4",
    "Revision": "a9322b9c7",
    "Server": false,
    "Version": "1.8.3"
  }
}`
	ServiceTestAnswer = `
[{
  "AggregatedStatus": "passing",
  "Service": {
    "ID": "test-id-1234",
    "Service": "test",
    "Tags": ["tag1"],
    "Address": "",
    "Meta": {"version":"1.0.0","environment":"staging"},
    "Port": 3341,
    "Weights": {
      "Passing": 1,
      "Warning": 1
    },
    "EnableTagOverride": false,
    "ProxyDestination": "",
    "Proxy": {},
    "Connect": {},
    "CreateIndex": 1,
    "ModifyIndex": 1
  },
  "Checks": [{
    "Node": "node1",
    "CheckID": "serfHealth",
    "Name": "Serf Health Status",
    "Status": "passing"
  }]
}]`
	ServiceOtherAnswer = `
[{
  "AggregatedStatus": "passing",
  "Service": {
    "ID": "other-id-5678",
    "Service": "other",
    "Tags": ["tag2"],
    "Address": "",
    "Meta": {"version":"1.0.0","environment":"staging"},
    "Port": 0,
    "Weights": {
      "Passing": 1,
      "Warning": 1
    },
    "EnableTagOverride": false,
    "ProxyDestination": "",
    "Proxy": {},
    "Connect": {},
    "CreateIndex": 1,
    "ModifyIndex": 1
  },
  "Checks": [{
    "Node": "node1",
    "CheckID": "serfHealth",
    "Name": "Serf Health Status",
    "Status": "passing"
  }]
}]`

	ServicesTestAnswer = `
{
  "test-id-1234": {
    "ID": "test-id-1234",
    "Service": "test",
    "Tags": [ "tag1" ],
    "Meta": {"version":"1.0.0","environment":"staging"},
    "Port": 3341,
    "Address": "1.1.1.1",
    "TaggedAddresses": {
      "lan_ipv4": {
        "Address": "1.1.1.1",
        "Port": 4646
      },
      "wan_ipv4": {
        "Address": "1.1.1.1",
        "Port": 4646
      }
    },
    "Weights": {
      "Passing": 1,
      "Warning": 1
    },
    "EnableTagOverride": false
  },
  "test-id-5678": {
    "ID": "test-id-5678",
    "Service": "test",
    "Tags": [ "tag1" ],
    "Meta": {"version":"1.0.0","environment":"staging"},
    "Port": 3341,
    "Address": "1.1.2.2",
    "TaggedAddresses": {
      "lan_ipv4": {
        "Address": "1.1.2.2",
        "Port": 4646
      },
      "wan_ipv4": {
        "Address": "1.1.2.2",
        "Port": 4646
      }
    },
    "Weights": {
      "Passing": 1,
      "Warning": 1
    },
    "EnableTagOverride": false
  },
  "other-id-9876": {
    "ID": "other-id-9876",
    "Service": "other",
    "Tags": [ "tag2" ],
    "Meta": {"version":"1.0.0","environment":"staging"},
    "Port": 0,
    "Address": "",
    "Weights": {
      "Passing": 1,
      "Warning": 1
    },
    "EnableTagOverride": false
  }
}`
)

func newServer(t *testing.T) (*httptest.Server, *SDConfig) {
	// github.com/hashicorp/consul/testutil/ would be nice but it needs a local consul binary.
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := ""
		switch r.URL.String() {
		case "/v1/agent/self":
			response = AgentAnswer
		case "/v1/agent/health/service/name/test?format=json":
			response = ServiceTestAnswer
		case "/v1/agent/health/service/name/other?format=json":
			response = ServiceOtherAnswer
		case "/v1/agent/services":
			response = ServicesTestAnswer
		default:
			t.Errorf("Unhandled consul call: %s", r.URL)
		}
		w.Header().Add("X-Consul-Index", "1")
		_, err := w.Write([]byte(response))
		require.NoError(t, err)
	}))
	stuburl, err := url.Parse(stub.URL)
	require.NoError(t, err)

	config := &SDConfig{
		Server:          stuburl.Host,
		Token:           "fake-token",
		RefreshInterval: model.Duration(1 * time.Second),
	}
	return stub, config
}

func newDiscovery(t *testing.T, config *SDConfig) *Discovery {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	metrics := NewTestMetrics(t, config, prometheus.NewRegistry())
	d, err := NewDiscovery(config, logger, metrics)
	require.NoError(t, err)
	return d
}

func checkOneTarget(t *testing.T, tg []*targetgroup.Group) {
	require.Equal(t, 1, len(tg))
	target := tg[0]
	require.Equal(t, "test-dc", string(target.Labels["__meta_consulagent_dc"]))
	require.Equal(t, target.Source, string(target.Labels["__meta_consulagent_service"]))
	if target.Source == "test" {
		// test service should have one node.
		require.Greater(t, len(target.Targets), 0, "Test service should have one node")
	}
}

// Watch all the services in the catalog.
func TestAllServices(t *testing.T) {
	stub, config := newServer(t)
	defer stub.Close()

	d := newDiscovery(t, config)

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan []*targetgroup.Group)
	go func() {
		d.Run(ctx, ch)
		close(ch)
	}()
	checkOneTarget(t, <-ch)
	checkOneTarget(t, <-ch)
	cancel()
	<-ch
}

// TestNoTargets with no targets is emitted if no services were discovered.
func TestNoTargets(t *testing.T) {
	stub, config := newServer(t)
	defer stub.Close()
	config.ServiceTags = []string{"missing"}

	d := newDiscovery(t, config)

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan []*targetgroup.Group)
	go d.Run(ctx, ch)

	targets := (<-ch)[0].Targets
	require.Equal(t, 0, len(targets))
	cancel()
}

// Watch only the test service.
func TestOneService(t *testing.T) {
	stub, config := newServer(t)
	defer stub.Close()

	config.Services = []string{"test"}
	d := newDiscovery(t, config)

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan []*targetgroup.Group)
	go d.Run(ctx, ch)
	checkOneTarget(t, <-ch)
	cancel()
}

// Watch the test service with a specific tag and node-meta.
func TestAllOptions(t *testing.T) {
	stub, config := newServer(t)
	defer stub.Close()

	config.Services = []string{"test"}
	config.NodeMeta = map[string]string{"rack_name": "2304"}
	config.ServiceTags = []string{"tag1"}
	config.AllowStale = true
	config.Token = "fake-token"

	d := newDiscovery(t, config)

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan []*targetgroup.Group)
	go func() {
		d.Run(ctx, ch)
		close(ch)
	}()
	checkOneTarget(t, <-ch)
	cancel()
	<-ch
}

func TestGetDatacenterShouldReturnError(t *testing.T) {
	for _, tc := range []struct {
		handler    func(http.ResponseWriter, *http.Request)
		errMessage string
	}{
		{
			// Define a handler that will return status 500.
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(500)
			},
			errMessage: "Unexpected response code: 500 ()",
		},
		{
			// Define a handler that will return incorrect response.
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, err := w.Write([]byte(`{"Config": {"Not-Datacenter": "test-dc"}}`))
				require.NoError(t, err)
			},
			errMessage: "invalid value '<nil>' for Config.Datacenter",
		},
	} {
		stub := httptest.NewServer(http.HandlerFunc(tc.handler))
		stuburl, err := url.Parse(stub.URL)
		require.NoError(t, err)

		config := &SDConfig{
			Server:          stuburl.Host,
			Token:           "fake-token",
			RefreshInterval: model.Duration(1 * time.Second),
		}
		defer stub.Close()
		d := newDiscovery(t, config)

		// Should be empty if not initialized.
		require.Equal(t, "", d.clientDatacenter)

		err = d.getDatacenter()

		// An error should be returned.
		require.Equal(t, tc.errMessage, err.Error())
		// Should still be empty.
		require.Equal(t, "", d.clientDatacenter)
	}
}
