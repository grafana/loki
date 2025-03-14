package gcplog

import (
	"flag"
	"fmt"
	"net"
	"os"
	"reflect"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/assert"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"

	"github.com/grafana/dskit/server"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
)

func TestNewGCPLogTarget(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	// Create fake promtail client
	eh := fake.New(func() {})
	defer eh.Stop()

	type args struct {
		metrics *Metrics
		logger  log.Logger
		handler api.EntryHandler
		relabel []*relabel.Config
		jobName string
		config  *scrapeconfig.GcplogTargetConfig
	}
	tests := []struct {
		name     string
		args     args
		wantType interface{}
		wantErr  assert.ErrorAssertionFunc
	}{
		{
			name: "defaults to pull target",
			args: args{
				metrics: NewMetrics(prometheus.NewRegistry()),
				logger:  logger,
				handler: eh,
				relabel: nil,
				jobName: "test_job_defaults_to_pull_target",
				config: &scrapeconfig.GcplogTargetConfig{
					ProjectID:        "test",
					SubscriptionType: "",
				},
			},
			wantType: &pullTarget{},
			wantErr:  assert.NoError,
		},
		{
			name: "pull SubscriptionType creates new pull target",
			args: args{
				metrics: NewMetrics(prometheus.NewRegistry()),
				logger:  logger,
				handler: eh,
				relabel: nil,
				jobName: "test_job_pull_subscriptiontype_creates_new",
				config: &scrapeconfig.GcplogTargetConfig{
					ProjectID:        "test",
					SubscriptionType: "pull",
				},
			},
			wantType: &pullTarget{},
			wantErr:  assert.NoError,
		},
		{
			name: "push SubscriptionType creates new pull target",
			args: args{
				metrics: NewMetrics(prometheus.NewRegistry()),
				logger:  logger,
				handler: eh,
				relabel: nil,
				jobName: "test_job_push_subscription_creates_new",
				config: &scrapeconfig.GcplogTargetConfig{
					ProjectID:        "test",
					SubscriptionType: "push",
				},
			},
			wantType: &pushTarget{},
			wantErr:  assert.NoError,
		},
		{
			name: "unknown subscription type fails to create target",
			args: args{
				metrics: NewMetrics(prometheus.NewRegistry()),
				logger:  logger,
				handler: eh,
				relabel: nil,
				jobName: "test_job_unknown_substype_fails_to_create_target",
				config: &scrapeconfig.GcplogTargetConfig{
					ProjectID:        "test",
					SubscriptionType: "magic",
				},
			},
			wantErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.ErrorContains(t, err, "invalid subscription type: magic", i...)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Since the push target underlying http server registers metrics in the default registerer, we have to override it to prevent duplicate metrics errors.
			prometheus.DefaultRegisterer = prometheus.NewRegistry()

			serverConfig, _, err := GetServerConfigWithAvailablePort()
			if err != nil {
				t.Fatal(err)
			}
			tt.args.config.Server = serverConfig
			got, err := NewGCPLogTarget(tt.args.metrics, tt.args.logger, tt.args.handler, tt.args.relabel, tt.args.jobName, tt.args.config, option.WithCredentials(&google.Credentials{}))
			// If the target was started, stop it after test
			if got != nil {
				defer func() { _ = got.Stop() }()
			}

			if !tt.wantErr(t, err, fmt.Sprintf("NewGCPLogTarget(%v, %v, %v, %v, %v, %v)", tt.args.metrics, tt.args.logger, tt.args.handler, tt.args.relabel, tt.args.jobName, tt.args.config)) {
				return
			}
			if err == nil {
				assert.IsType(t, tt.wantType, got, "created target type different than expected: Got: %s", reflect.TypeOf(got).Name())
			}
		})
	}
}

const localhost = "127.0.0.1"

func GetServerConfigWithAvailablePort() (cfg server.Config, port int, err error) {
	// Get a randomly available port by open and closing a TCP socket
	addr, err := net.ResolveTCPAddr("tcp", localhost+":0")
	if err != nil {
		return
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return
	}
	port = l.Addr().(*net.TCPAddr).Port
	err = l.Close()
	if err != nil {
		return
	}

	// Adjust some of the defaults
	cfg.RegisterFlags(flag.NewFlagSet("empty", flag.ContinueOnError))
	cfg.HTTPListenAddress = localhost
	cfg.HTTPListenPort = port
	cfg.GRPCListenAddress = localhost
	cfg.GRPCListenPort = 0 // Not testing GRPC, a random port will be assigned

	return
}
