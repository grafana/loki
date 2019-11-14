package targets

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/relabel"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"

	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/scrape"
	"github.com/prometheus/common/model"
)

var (
	httpEntries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "http_target_entries_total",
		Help:      "Total number of successful entries sent to HTTP target",
	})
)

// HTTPTarget listens to HTTP requests and handles them as if they were
// tailed from a file.
type HTTPTarget struct {
	logger        log.Logger
	handler       api.EntryHandler
	relabelConfig []*relabel.Config
	labels        model.LabelSet
}

// NewHTTPTarget configures a new HTTPTarget.
func NewHTTPTarget(
	logger log.Logger,
	handler api.EntryHandler,
	relabel []*relabel.Config,
	config *scrape.HTTPTargetConfig,
) (*HTTPTarget, error) {

	return &HTTPTarget{
		logger:        logger,
		handler:       handler,
		relabelConfig: relabel,
		labels:        config.Labels,
	}, nil
}

// ServeHTTP implements http.Handler, and is invoked by promtail's server
// whenever a write request comes in, generating logs to eventually send
// to Loki.
//
// Logs are expected to come in with the URL path of label pairs separated
// by slashes: /label1/value1/label2/value2 (and so on). A RFC3339 or
// RFC3339Nano timestamp can be also by provided by adding `?ts=<timestamp>`
// as a query parameter.
func (t *HTTPTarget) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	pathLabels, err := labelsFromPath(req.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	lbls, err := t.transformLabels(pathLabels)
	if err != nil {
		level.Error(t.logger).Log("msg", "failed to get labels for message", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	} else if len(lbls) == 0 {
		// Drop entry, no labels.
		w.WriteHeader(200)
		return
	}

	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		level.Error(t.logger).Log("msg", "failed to read request body", "err", err)
		http.Error(w, "could not read message body", http.StatusInternalServerError)
		return
	} else if len(body) == 0 {
		http.Error(w, "no log message provided", http.StatusBadRequest)
		return
	}

	messageTime := time.Now()
	if ts := req.URL.Query().Get("ts"); ts != "" {
		var err error
		messageTime, err = time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			http.Error(w, fmt.Sprintf("could not read log timestamp: %v", err), http.StatusBadRequest)
			return
		}
	}

	t.handler.Handle(lbls, messageTime, string(body))
	httpEntries.Inc()

	w.WriteHeader(200)
}

func (t *HTTPTarget) transformLabels(lbls map[string]string) (model.LabelSet, error) {
	// Add constant labels
	for k, v := range t.labels {
		lbls[string(k)] = string(v)
	}

	processed := relabel.Process(labels.FromMap(lbls), t.relabelConfig...).Map()

	ret := make(model.LabelSet)
	for k, v := range processed {
		if k[0:2] == "__" {
			continue
		}
		ret[model.LabelName(k)] = model.LabelValue(v)
	}

	return ret, nil
}

// Type returns HTTPTargetType.
func (t *HTTPTarget) Type() TargetType {
	return HTTPTargetType
}

// Ready indicates whether or not the journal is ready to be read from.
func (t *HTTPTarget) Ready() bool {
	return true
}

// DiscoveredLabels returns the set of labels discovered by the HTTPTarget, which
// is always nil. Implements Target.
func (t *HTTPTarget) DiscoveredLabels() model.LabelSet {
	return nil
}

// Labels returns the set of labels that statically apply to all log entries
// produced by the HTTPTarget.
func (t *HTTPTarget) Labels() model.LabelSet {
	return t.labels
}

// Details returns target-specific details.
func (t *HTTPTarget) Details() interface{} {
	return map[string]string{}
}

// Stop shuts down the HTTPTarget.
func (t *HTTPTarget) Stop() error {
	return nil
}

func labelsFromPath(path string) (map[string]string, error) {
	var pairs []string
	for _, part := range strings.Split(path, "/") {
		if part == "" {
			continue
		}
		pairs = append(pairs, strings.TrimSpace(part))
	}

	if len(pairs)%2 != 0 {
		return nil, fmt.Errorf("missing label value in path for %s", pairs[len(pairs)-1])
	}

	ls := make(map[string]string)
	for i := 0; i < len(pairs); i += 2 {
		ls[pairs[i]] = pairs[i+1]
	}

	return ls, nil
}
