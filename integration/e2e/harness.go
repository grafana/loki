//go:build e2e

package e2e

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/grafana/e2e"
	e2edb "github.com/grafana/e2e/db"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

const (
	// defaultImage is the image `make test-e2e` builds. Set the LOKI_E2E_IMAGE environment
	// variable to test another image.
	defaultImage = "grafana/loki:e2e"

	httpPort   = 3100
	grpcPort   = 9095
	memberPort = 7946
	minioPort  = 9000
	kafkaPort  = 9092

	// tenant is the tenant the tests write to and read from.
	tenant = "smoke"
)

//go:embed single.yaml
var singleConfig string

//go:embed distributed.yaml
var distributedConfig string

// mode is a way to deploy Loki.
type mode string

const (
	// singleBinary runs the whole of Loki in one container.
	singleBinary mode = "single-binary"
	// distributed runs each component in its own container, with Kafka between the write and
	// the read path.
	distributed mode = "distributed"
)

var allModes = []mode{singleBinary, distributed}

// harness is a running Loki deployment.
type harness struct {
	mode     mode
	scenario *e2e.Scenario

	// writeURL and readURL are the base URLs of the component that takes pushes and of the one
	// that takes queries. In single binary mode they are the same.
	writeURL string
	readURL  string
}

// forEachMode runs fn as a subtest against every deployment mode, each with its own deployment.
func forEachMode(t *testing.T, fn func(t *testing.T, h *harness)) {
	t.Helper()
	for _, m := range allModes {
		t.Run(string(m), func(t *testing.T) {
			fn(t, startHarness(t, m))
		})
	}
}

func lokiImage() string {
	if image := os.Getenv("LOKI_E2E_IMAGE"); image != "" {
		return image
	}
	return defaultImage
}

// startHarness starts a deployment and waits until it is ready. It stops when the test ends.
func startHarness(t *testing.T, m mode) *harness {
	t.Helper()

	// Docker network names cannot contain a slash, which subtest names do.
	name := "e2e-loki-" + strings.NewReplacer("/", "-", "_", "-").Replace(strings.ToLower(t.Name()))
	if len(name) > 60 {
		name = name[:60]
	}
	scenario, err := e2e.NewScenario(name)
	require.NoError(t, err)
	t.Cleanup(scenario.Close)

	h := &harness{mode: m, scenario: scenario}
	switch m {
	case singleBinary:
		h.startSingleBinary(t)
	case distributed:
		h.startDistributed(t)
	default:
		t.Fatalf("unknown mode %q", m)
	}
	return h
}

func (h *harness) startSingleBinary(t *testing.T) {
	writeConfig(t, h.scenario, singleConfig, nil)

	loki := newLoki("loki", "-target=all", "-lbac.enabled=true")
	require.NoError(t, h.scenario.StartAndWaitReady(loki))

	h.writeURL = "http://" + loki.Endpoint(httpPort)
	h.readURL = h.writeURL
}

func (h *harness) startDistributed(t *testing.T) {
	kafka := e2edb.NewKafka()
	minio := e2edb.NewMinio(minioPort, "loki")
	require.NoError(t, h.scenario.StartAndWaitReady(kafka, minio))
	waitForKafkaCoordinator(t, kafka.Endpoint(kafkaPort))

	var (
		distributor     = newLoki("distributor")
		ingester        = newLoki("ingester")
		patternIngester = newLoki("pattern-ingester")
		compactor       = newLoki("compactor", "-target=compactor")
		scheduler       = newLoki("query-scheduler")
		querier         = newLoki("querier")
		frontend        = newLoki("query-frontend")
	)
	writeConfig(t, h.scenario, distributedConfig, map[string]string{
		"Minio":          minio.NetworkEndpoint(minioPort),
		"MinioAccessKey": e2edb.MinioAccessKey,
		"MinioSecretKey": e2edb.MinioSecretKey,
		"Compactor":      compactor.host(h.scenario),
		"Ingester":       ingester.host(h.scenario),
		"Scheduler":      scheduler.host(h.scenario),
	})

	kafkaAddr := kafka.NetworkEndpoint(kafkaPort)
	lbacFlags := []string{"-lbac.enabled=true"}
	kafkaFlags := []string{
		"-kafka.writer.address=" + kafkaAddr,
		"-kafka.reader.address=" + kafkaAddr,
		"-kafka.topic=loki",
		"-ingester.kafka-ingestion-enabled=true",
		"-ingester.partition-ring.store=memberlist",
	}
	distributor.args = append([]string{
		"-target=distributor",
		"-distributor.kafka-writes-enabled=true",
		"-distributor.ingester-writes-enabled=false",
	}, kafkaFlags...)
	ingester.args = append([]string{
		"-target=ingester",
		"-ingester.lifecycler.ID=ingester-0", // a fixed ID makes this the owner of partition 0
		"-ingester.partition-ring.min-partition-owners-duration=0s",
	}, kafkaFlags...)
	patternIngester.args = []string{"-target=pattern-ingester"}
	scheduler.args = []string{"-target=query-scheduler"}
	querier.args = append([]string{"-target=querier"}, kafkaFlags...)
	// The query-frontend cannot tail on its own, so it forwards tail requests to a querier.
	frontend.args = []string{"-target=query-frontend", "-frontend.tail-proxy-url=http://" + querier.host(h.scenario) + ":3100"}

	// The query-frontend only becomes ready once a querier is connected to the scheduler, so
	// wait for all of them together.
	services := []*lokiService{compactor, distributor, ingester, patternIngester, scheduler, querier, frontend}
	for _, s := range services {
		s.args = append(s.args, lbacFlags...)
		s.build()
	}
	require.NoError(t, h.scenario.Start(asServices(services)...))
	require.NoError(t, h.scenario.WaitReady(asServices(services)...))

	h.writeURL = "http://" + distributor.Endpoint(httpPort)
	h.readURL = "http://" + frontend.Endpoint(httpPort)
}

// waitForKafkaCoordinator waits until Kafka can serve consumer groups. A broker answers metadata
// requests as soon as it starts, but it creates the topic that stores consumer group offsets only
// on the first request for a group, and a consumer that asks before then fails to start.
func waitForKafkaCoordinator(t *testing.T, addr string) {
	t.Helper()

	client, err := kgo.NewClient(kgo.SeedBrokers(addr))
	require.NoError(t, err)
	defer client.Close()

	require.Eventually(t, func() bool {
		req := kmsg.NewPtrFindCoordinatorRequest()
		req.CoordinatorKey = "smoke-test-warmup"
		resp, err := req.RequestWith(t.Context(), client)
		return err == nil && resp.ErrorCode == 0 && (len(resp.Coordinators) == 0 || resp.Coordinators[0].ErrorCode == 0)
	}, 30*time.Second, 500*time.Millisecond, "Kafka did not start serving consumer groups")
}

func asServices(ls []*lokiService) []e2e.Service {
	out := make([]e2e.Service, len(ls))
	for i, l := range ls {
		out[i] = l.HTTPService
	}
	return out
}

// lokiService is a Loki container. Its command line is filled in after it is created, because
// the config and the command lines of some services refer to the hostnames of others.
type lokiService struct {
	*e2e.HTTPService
	name string
	args []string
}

func newLoki(name string, args ...string) *lokiService {
	l := &lokiService{name: name, args: args}
	l.build()
	return l
}

// build creates the container from the current arguments.
func (l *lokiService) build() {
	// The image's entrypoint is the loki binary, so the command line is only flags. The first
	// flag takes the place of the command.
	config := "-config.file=" + filepath.Join(e2e.ContainerSharedDir, "loki.yaml")
	l.HTTPService = e2e.NewHTTPService(
		l.name,
		lokiImage(),
		e2e.NewCommand(config, l.args...),
		e2e.NewHTTPReadinessProbe(httpPort, "/ready", 200, 299),
		httpPort, grpcPort, memberPort,
	)
}

// host is the hostname of the service within the scenario's network.
func (l *lokiService) host(s *e2e.Scenario) string {
	return e2e.NetworkContainerHost(s.NetworkName(), l.name)
}

// writeConfig renders a config template to the directory the containers share.
func writeConfig(t *testing.T, s *e2e.Scenario, text string, data map[string]string) {
	t.Helper()

	tmpl, err := template.New("loki.yaml").Option("missingkey=error").Parse(text)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, tmpl.Execute(&buf, data))
	require.NoError(t, os.WriteFile(filepath.Join(s.SharedDir(), "loki.yaml"), buf.Bytes(), 0o644))
}

// request is an HTTP request to Loki.
type request struct {
	method      string
	url         string
	query       url.Values
	body        string
	contentType string
	headers     map[string]string
}

// do sends req as the test tenant and returns the status code and the body.
func do(t *testing.T, req request) (int, string) {
	t.Helper()

	if req.method == "" {
		req.method = http.MethodGet
	}
	target := req.url
	if len(req.query) > 0 {
		target += "?" + req.query.Encode()
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, req.method, target, strings.NewReader(req.body))
	require.NoError(t, err)
	httpReq.Header.Set("X-Scope-OrgID", tenant)
	if req.contentType != "" {
		httpReq.Header.Set("Content-Type", req.contentType)
	}
	for k, v := range req.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err, "%s %s", req.method, target)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

// stream is a log stream with a single line.
type stream struct {
	labels map[string]string
	line   string
}

// pushLoki writes the streams, one entry each, through the Loki push API.
func (h *harness) pushLoki(t *testing.T, ts time.Time, streams ...stream) {
	t.Helper()

	values := make([]string, 0, len(streams))
	for _, s := range streams {
		labels := make([]string, 0, len(s.labels))
		for k, v := range s.labels {
			labels = append(labels, fmt.Sprintf("%q:%q", k, v))
		}
		values = append(values, fmt.Sprintf(`{"stream":{%s},"values":[["%d",%q]]}`,
			strings.Join(labels, ","), ts.UnixNano(), s.line))
	}
	status, body := do(t, request{
		method:      http.MethodPost,
		url:         h.writeURL + "/loki/api/v1/push",
		body:        `{"streams":[` + strings.Join(values, ",") + `]}`,
		contentType: "application/json",
	})
	require.Equal(t, http.StatusNoContent, status, body)
}

// pushOTLP writes one log record through the OTLP push API.
func (h *harness) pushOTLP(t *testing.T, ts time.Time, service, line string) {
	t.Helper()

	body := fmt.Sprintf(`{"resourceLogs":[{
		"resource":{"attributes":[{"key":"service.name","value":{"stringValue":%q}}]},
		"scopeLogs":[{"logRecords":[{"timeUnixNano":"%d","body":{"stringValue":%q}}]}]}]}`,
		service, ts.UnixNano(), line)
	status, resp := do(t, request{
		method:      http.MethodPost,
		url:         h.writeURL + "/otlp/v1/logs",
		body:        body,
		contentType: "application/json",
	})
	require.Equal(t, http.StatusNoContent, status, resp)
}

// pushQuietly is pushLoki for use outside the test goroutine. It ignores errors, because a missed
// write only delays the check that waits for it.
func pushQuietly(h *harness, ts time.Time, streams ...stream) {
	values := make([]string, 0, len(streams))
	for _, s := range streams {
		labels := make([]string, 0, len(s.labels))
		for k, v := range s.labels {
			labels = append(labels, fmt.Sprintf("%q:%q", k, v))
		}
		values = append(values, fmt.Sprintf(`{"stream":{%s},"values":[["%d",%q]]}`,
			strings.Join(labels, ","), ts.UnixNano(), s.line))
	}
	req, err := http.NewRequest(http.MethodPost, h.writeURL+"/loki/api/v1/push",
		strings.NewReader(`{"streams":[`+strings.Join(values, ",")+`]}`))
	if err != nil {
		return
	}
	req.Header.Set("X-Scope-OrgID", tenant)
	req.Header.Set("Content-Type", "application/json")
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
	}
}
