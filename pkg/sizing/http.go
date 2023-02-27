package sizing

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"gopkg.in/yaml.v2"
)

type Message struct {
	NodeType         NodeType
	Ingest           int
	Retention        int
	QueryPerformance QueryPerf
}

func decodeMesage(req *http.Request, msg *Message) error {
	var err error
	types := strings.Split(req.FormValue("node-type"), " - ")
	nodeTypes, ok := NodeTypesByProvider[types[0]]
	if !ok {
		return fmt.Errorf("unknown cloud provider %s", types[0])
	}
	msg.NodeType, ok = nodeTypes[types[1]]
	if !ok {
		return fmt.Errorf("unknown node type %s", types[1])
	}

	msg.Ingest, err = strconv.Atoi(req.FormValue("ingest"))
	if err != nil {
		return fmt.Errorf("cannot read ingest: %w", err)
	}

	msg.Retention, err = strconv.Atoi(req.FormValue("retention"))
	if err != nil {
		return fmt.Errorf("cannot read retention: %w", err)
	}

	msg.QueryPerformance = QueryPerf(strings.ToLower(req.FormValue("queryperf")))

	return nil
}

// Handler defines the REST API of the sizing tool.
type Handler struct {
	logger log.Logger
}

func NewHandler(logger log.Logger) *Handler {
	return &Handler{logger: logger}
}

func (h *Handler) GenerateHelmValues(w http.ResponseWriter, req *http.Request) {

	var msg Message
	err := decodeMesage(req, &msg)
	if err != nil {
		level.Error(h.logger).Log("error", err)
		h.respondError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/x-yaml; charset=utf-8")

	cluster := calculateClusterSize(msg.NodeType, float64(msg.Ingest), msg.QueryPerformance)
	helm := constructHelmValues(cluster, msg.NodeType)

	enc := yaml.NewEncoder(w)
	err = enc.Encode(helm)
	if err != nil {
		level.Error(h.logger).Log("msg", "could not encode Helm Chart values", "error", err)
	}
}

func (h *Handler) Nodes(w http.ResponseWriter, req *http.Request) {
	var nodes []string
	for cloud, n := range NodeTypesByProvider {
		for nodeType := range n {
			nodes = append(nodes, fmt.Sprintf("%s - %s", cloud, nodeType))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(nodes)
	if err != nil {
		level.Error(h.logger).Log("msg", "could not encode node values", "error", err)
	}
}

func (h *Handler) respondError(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusBadRequest)
	_, err = w.Write([]byte(fmt.Sprintf("error: %v", err)))
	if err != nil {
		level.Error(h.logger).Log("msg", "could not write error message", "error", err)
	}
}

func (h *Handler) Cluster(w http.ResponseWriter, req *http.Request) {
	var msg Message

	err := decodeMesage(req, &msg)
	if err != nil {
		level.Error(h.logger).Log("error", err)
		h.respondError(w, err)
		return
	}

	cluster := calculateClusterSize(msg.NodeType, float64(msg.Ingest), msg.QueryPerformance)

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(cluster)
	if err != nil {
		level.Error(h.logger).Log("msg", "could not encode cluster size", "error", err)
	}
}
