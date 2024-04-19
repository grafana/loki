package gcplog

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/grafana/dskit/backoff"
	"github.com/pkg/errors"

	"cloud.google.com/go/pubsub"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/target"
)

func TestPullTarget_RunStop(t *testing.T) {
	t.Run("it sends messages to the promclient and stops when Stop() is called", func(t *testing.T) {
		tc := testPullTarget(t)

		runErr := make(chan error)
		go func() {
			runErr <- tc.target.run()
		}()

		tc.sub.messages <- &pubsub.Message{Data: []byte(gcpLogEntry)}
		require.Eventually(t, func() bool {
			return len(tc.promClient.Received()) > 0
		}, time.Second, 50*time.Millisecond)

		require.NoError(t, tc.target.Stop())
		require.EqualError(t, <-runErr, "context canceled")
	})

	t.Run("it retries when there is an error", func(t *testing.T) {
		tc := testPullTarget(t)

		runErr := make(chan error)
		go func() {
			runErr <- tc.target.run()
		}()

		tc.sub.errors <- errors.New("something bad")
		tc.sub.messages <- &pubsub.Message{Data: []byte(gcpLogEntry)}
		require.Eventually(t, func() bool {
			return len(tc.promClient.Received()) > 0
		}, time.Second, 50*time.Millisecond)

		require.NoError(t, tc.target.Stop())

		require.Eventually(t, func() bool {
			select {
			case e := <-runErr:
				return e.Error() == "context canceled"
			default:
				return false
			}
		}, time.Second, 50*time.Millisecond)
	})

	t.Run("a successful message resets retries", func(t *testing.T) {
		tc := testPullTarget(t)

		runErr := make(chan error)
		go func() {
			runErr <- tc.target.run()
		}()

		tc.sub.errors <- errors.New("something bad")
		tc.sub.errors <- errors.New("something bad")
		tc.sub.errors <- errors.New("something bad")
		tc.sub.errors <- errors.New("something bad")
		tc.sub.messages <- &pubsub.Message{Data: []byte(gcpLogEntry)}
		tc.sub.errors <- errors.New("something bad")
		tc.sub.errors <- errors.New("something bad")
		tc.sub.messages <- &pubsub.Message{Data: []byte(gcpLogEntry)}

		require.Eventually(t, func() bool {
			return len(tc.promClient.Received()) > 1
		}, time.Second, 50*time.Millisecond)

		require.NoError(t, tc.target.Stop())
	})
}

func TestPullTarget_Type(t *testing.T) {
	tc := testPullTarget(t)

	assert.Equal(t, target.TargetType("Gcplog"), tc.target.Type())
}

func TestPullTarget_Ready(t *testing.T) {
	tc := testPullTarget(t)

	assert.Equal(t, true, tc.target.Ready())
}

func TestPullTarget_Labels(t *testing.T) {
	tc := testPullTarget(t)

	assert.Equal(t, model.LabelSet{"job": "test-gcplogtarget"}, tc.target.Labels())
}

type testContext struct {
	target     *pullTarget
	promClient *fake.Client
	sub        *fakeSubscription
}

func testPullTarget(t *testing.T) *testContext {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	sub := newFakeSubscription()
	promClient := fake.New(func() {})
	target := &pullTarget{
		metrics:       NewMetrics(prometheus.NewRegistry()),
		logger:        log.NewNopLogger(),
		handler:       promClient,
		relabelConfig: nil,
		ctx:           ctx,
		cancel:        cancel,
		config:        testConfig,
		jobName:       t.Name() + "job-test-gcplogtarget",
		ps:            io.NopCloser(nil),
		sub:           sub,
		msgs:          make(chan *pubsub.Message),
		backoff:       backoff.New(ctx, testBackoff),
	}

	return &testContext{
		target:     target,
		promClient: promClient,
		sub:        sub,
	}
}

const (
	project      = "test-project"
	subscription = "test-subscription"
	gcpLogEntry  = `
{
  "insertId": "ajv4d1f1ch8dr",
  "logName": "projects/grafanalabs-dev/logs/cloudaudit.googleapis.com%2Fdata_access",
  "protoPayload": {
    "@type": "type.googleapis.com/google.cloud.audit.AuditLog",
    "authenticationInfo": {
      "principalEmail": "1040409107725-compute@developer.gserviceaccount.com",
      "serviceAccountDelegationInfo": [
        {
          "firstPartyPrincipal": {
            "principalEmail": "service-1040409107725@compute-system.iam.gserviceaccount.com"
          }
        }
      ]
    },
    "authorizationInfo": [
      {
        "granted": true,
        "permission": "storage.objects.list",
        "resource": "projects/_/buckets/dev-us-central1-cortex-tsdb-dev",
        "resourceAttributes": {
        }
      },
      {
        "permission": "storage.objects.get",
        "resource": "projects/_/buckets/dev-us-central1-cortex-tsdb-dev/objects/load-generator-20/01EM34PFBC2SCV3ETBGRAQZ090/deletion-mark.json",
        "resourceAttributes": {
        }
      }
    ],
    "methodName": "storage.objects.get",
    "requestMetadata": {
      "callerIp": "34.66.19.193",
      "callerNetwork": "//compute.googleapis.com/projects/grafanalabs-dev/global/networks/__unknown__",
      "callerSuppliedUserAgent": "thanos-store-gateway/1.5.0 (go1.14.9),gzip(gfe)",
      "destinationAttributes": {
      },
      "requestAttributes": {
        "auth": {
        },
        "time": "2021-01-01T02:17:10.661405637Z"
      }
    },
    "resourceLocation": {
      "currentLocations": [
        "us-central1"
      ]
    },
    "resourceName": "projects/_/buckets/dev-us-central1-cortex-tsdb-dev/objects/load-generator-20/01EM34PFBC2SCV3ETBGRAQZ090/deletion-mark.json",
    "serviceName": "storage.googleapis.com",
    "status": {
    }
  },
  "receiveTimestamp": "2021-01-01T02:17:10.82013623Z",
  "resource": {
    "labels": {
      "bucket_name": "dev-us-central1-cortex-tsdb-dev",
      "location": "us-central1",
      "project_id": "grafanalabs-dev"
    },
    "type": "gcs_bucket"
  },
  "severity": "INFO",
  "timestamp": "2021-01-01T02:17:10.655982344Z"
}
`
)

var testConfig = &scrapeconfig.GcplogTargetConfig{
	ProjectID:    project,
	Subscription: subscription,
	Labels: model.LabelSet{
		"job": "test-gcplogtarget",
	},
	SubscriptionType: "pull",
}

func newFakeSubscription() *fakeSubscription {
	return &fakeSubscription{
		messages: make(chan *pubsub.Message),
		errors:   make(chan error),
	}
}

type fakeSubscription struct {
	messages chan *pubsub.Message
	errors   chan error
}

func (s *fakeSubscription) Receive(ctx context.Context, f func(context.Context, *pubsub.Message)) error {
	for {
		select {
		case m := <-s.messages:
			f(ctx, m)
		case e := <-s.errors:
			return e
		}
	}
}

var testBackoff = backoff.Config{
	MinBackoff: 1 * time.Millisecond,
	MaxBackoff: 10 * time.Millisecond,
}
