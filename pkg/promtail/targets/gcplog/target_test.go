package gcplog

import (
	"context"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/pubsub"
	"cloud.google.com/go/pubsub/pstest"
	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/promtail/client/fake"
	"github.com/grafana/loki/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/pkg/promtail/targets/target"
)

func TestGcplogTarget_Run(t *testing.T) {
	// Goal: Check message written to pubsub topic is received by the target.
	tt, apiclient, pubsubClient, teardown := testGcplogTarget(t)
	defer teardown()

	// seed pubsub
	ctx := context.Background()
	tp, err := pubsubClient.CreateTopic(ctx, topic)
	require.NoError(t, err)
	_, err = pubsubClient.CreateSubscription(ctx, subscription, pubsub.SubscriptionConfig{
		Topic: tp,
	})
	require.NoError(t, err)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = tt.run()
	}()

	publishMessage(t, tp)

	// Wait till message is received by the run loop.
	// NOTE(kavi): sleep is not ideal. but not other way to confirm if api.Handler received messages
	time.Sleep(1 * time.Second)

	err = tt.Stop()
	require.NoError(t, err)

	// wait till `run` stops.
	wg.Wait()

	assert.Equal(t, 1, len(apiclient.Received()))
}

func TestGcplogTarget_Stop(t *testing.T) {
	// Goal: To test that `run()` stops when you invoke `target.Stop()`

	errs := make(chan error, 1)

	tt, _, _, teardown := testGcplogTarget(t)
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

func TestGcplogTarget_Type(t *testing.T) {
	tt, _, _, teardown := testGcplogTarget(t)
	defer teardown()

	assert.Equal(t, target.TargetType("Gcplog"), tt.Type())
}

func TestGcplogTarget_Ready(t *testing.T) {
	tt, _, _, teardown := testGcplogTarget(t)
	defer teardown()

	assert.Equal(t, true, tt.Ready())
}

func TestGcplogTarget_Labels(t *testing.T) {
	tt, _, _, teardown := testGcplogTarget(t)
	defer teardown()

	assert.Equal(t, model.LabelSet{"job": "test-gcplogtarget"}, tt.Labels())
}

func testGcplogTarget(t *testing.T) (*GcplogTarget, *fake.Client, *pubsub.Client, func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())

	mockSvr := pstest.NewServer()
	conn, err := grpc.Dial(mockSvr.Addr, grpc.WithInsecure())
	require.NoError(t, err)

	mockpubsubClient, err := pubsub.NewClient(ctx, testConfig.ProjectID, option.WithGRPCConn(conn))
	require.NoError(t, err)

	fakeClient := fake.New(func() {})

	target := newGcplogTarget(
		NewMetrics(prometheus.NewRegistry()),
		log.NewNopLogger(),
		fakeClient,
		nil,
		"job-test-gcplogtarget",
		testConfig,
		mockpubsubClient,
		ctx,
		cancel,
	)

	// cleanup
	return target, fakeClient, mockpubsubClient, func() {
		cancel()
		conn.Close()
		mockSvr.Close()
	}
}

func publishMessage(t *testing.T, topic *pubsub.Topic) {
	t.Helper()

	ctx := context.Background()
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

var (
	testConfig = &scrapeconfig.GcplogTargetConfig{
		ProjectID:    project,
		Subscription: subscription,
		Labels: model.LabelSet{
			"job": "test-gcplogtarget",
		},
	}
)
