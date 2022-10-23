package gcplog

import (
	"context"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/pubsub"
	"cloud.google.com/go/pubsub/pstest"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"
)

func TestPullTarget_Run(t *testing.T) {
	// Goal: Check message written to pubsub topic is received by the target.
	ctx := context.Background()
	tt, apiclient, pubsubClient, teardown := testPullTarget(ctx, t)
	defer teardown()

	// seed pubsub
	tp, err := pubsubClient.CreateTopic(ctx, topic)
	require.NoError(t, err)
	defer tp.Stop()

	_, err = pubsubClient.CreateSubscription(ctx, subscription, pubsub.SubscriptionConfig{
		Topic: tp,
	})
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		tt.run() //nolint:errcheck
	}()

	publishMessage(ctx, t, tp)
	// Wait till message is received by the run loop.
	// NOTE(kavi): sleep is not ideal. but not other way to confirm if api.Handler received messages
	time.Sleep(500 * time.Millisecond)

	err = tt.Stop()
	require.NoError(t, err)

	// wait till `run` stops.
	wg.Wait()

	// Sleep one more time before reading from api.Received.
	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, 1, len(apiclient.Received()))
}

func TestPullTarget_Stop(t *testing.T) {
	// Goal: To test that `run()` stops when you invoke `target.Stop()`

	errs := make(chan error, 1)

	ctx := context.Background()
	tt, _, _, teardown := testPullTarget(ctx, t)
	defer teardown()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		errs <- tt.run()
	}()

	// invoke stop
	_ = tt.Stop()

	// wait till run returns
	wg.Wait()

	// wouldn't block as 1 error is buffered into the channel.
	err := <-errs

	// returned error should be cancelled context error
	assert.Equal(t, tt.ctx.Err(), err)
}

func TestPullTarget_Type(t *testing.T) {
	ctx := context.Background()
	tt, _, _, teardown := testPullTarget(ctx, t)
	defer teardown()

	assert.Equal(t, target.TargetType("Gcplog"), tt.Type())
}

func TestPullTarget_Ready(t *testing.T) {
	ctx := context.Background()
	tt, _, _, teardown := testPullTarget(ctx, t)
	defer teardown()

	assert.Equal(t, true, tt.Ready())
}

func TestPullTarget_Labels(t *testing.T) {
	ctx := context.Background()
	tt, _, _, teardown := testPullTarget(ctx, t)
	defer teardown()

	assert.Equal(t, model.LabelSet{"job": "test-gcplogtarget"}, tt.Labels())
}

func testPullTarget(ctx context.Context, t *testing.T) (*pullTarget, *fake.Client, *pubsub.Client, func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(ctx)

	mockSvr := pstest.NewServer()
	conn, err := grpc.Dial(mockSvr.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	mockpubsubClient, err := pubsub.NewClient(ctx, testConfig.ProjectID, option.WithGRPCConn(conn))
	require.NoError(t, err)

	fakeClient := fake.New(func() {})

	var handler api.EntryHandler = fakeClient
	target := &pullTarget{
		metrics:       NewMetrics(prometheus.NewRegistry()),
		logger:        log.NewNopLogger(),
		handler:       handler,
		relabelConfig: nil,
		config:        testConfig,
		jobName:       t.Name() + "job-test-gcplogtarget",
		ctx:           ctx,
		cancel:        cancel,
		ps:            mockpubsubClient,
		msgs:          make(chan *pubsub.Message),
	}

	// cleanup
	return target, fakeClient, mockpubsubClient, func() {
		cancel()
		conn.Close()
		mockSvr.Close()
		mockpubsubClient.Close()
	}
}

func publishMessage(ctx context.Context, t *testing.T, topic *pubsub.Topic) {
	t.Helper()

	res := topic.Publish(ctx, &pubsub.Message{Data: []byte(gcpLogEntry)})

	_, err := res.Get(ctx) // wait till message is actully published
	require.NoError(t, err)
}

const (
	project      = "test-project"
	topic        = "test-topic"
	subscription = "test-subscription"

	gcpLogEntry = `
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
