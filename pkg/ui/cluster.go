package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/go-kit/log/level"
	"go.yaml.in/yaml/v3"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/dskit/ring"

	"github.com/grafana/loki/v3/pkg/analytics"
)

// Cluster represents a collection of cluster members.
type Cluster struct {
	Members map[string]Member `json:"members"`
}

// Member represents a node in the cluster with its current state and capabilities.
type Member struct {
	Addr     string         `json:"addr"`
	Target   string         `json:"target"`
	Services []ServiceState `json:"services"`
	Build    BuildInfo      `json:"build"`
	Error    error          `json:"error,omitempty"`
	Ready    ReadyResponse  `json:"ready,omitempty"`

	configBody string
}

// ServiceState represents the current state of a service running on a member.
type ServiceState struct {
	Service string `json:"service"`
	Status  string `json:"status"`
}

// BuildInfo contains version and build information about a member.
type BuildInfo struct {
	Version   string `json:"version"`
	Revision  string `json:"revision"`
	Branch    string `json:"branch"`
	BuildUser string `json:"buildUser"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
}

// fetchClusterMembers retrieves the state of all members in the cluster.
// It uses an errgroup to fetch member states concurrently with a limit of 16 concurrent operations.
func (s *Service) fetchClusterMembers(ctx context.Context) (Cluster, error) {
	var cluster Cluster
	cluster.Members = make(map[string]Member)

	// Discover all unique instances from rings
	instances := s.discoverInstances()

	if len(instances) == 0 {
		return cluster, nil
	}

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(16)

	// Use a mutex to protect concurrent map access
	var mu sync.Mutex

	for id, addr := range instances {
		instanceID, instanceAddr := id, addr // Create new variables to avoid closure issues
		g.Go(func() error {
			member, err := s.fetchMemberState(ctx, instanceAddr)
			if err != nil {
				member.Error = err
			}
			mu.Lock()
			cluster.Members[instanceID] = member
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return Cluster{}, fmt.Errorf("fetching cluster members: %w", err)
	}
	return cluster, nil
}

// discoverInstances queries the ring and returns a map of instance ID to HTTP address.
func (s *Service) discoverInstances() map[string]string {
	instances := make(map[string]string)

	// Query the ring for all healthy instances
	replicationSet, err := s.ring.GetAllHealthy(ring.Read)
	if err != nil {
		level.Warn(s.logger).Log("msg", "failed to get instances from ring", "err", err)
		return instances
	}

	// Collect all instance addresses
	for _, instance := range replicationSet.Instances {
		// Use instance ID as the key, and Addr as the HTTP address
		instances[instance.Id] = instance.Addr
	}

	return instances
}

// fetchMemberState retrieves the complete state of a single cluster member by instance ID and address.
// The addr parameter should be the full HTTP address (host:port) from the ring.
func (s *Service) fetchMemberState(ctx context.Context, addr string) (Member, error) {
	member := Member{
		Addr: addr,
	}

	config, err := s.fetchConfig(ctx, addr)
	if err != nil {
		return member, fmt.Errorf("fetching config: %w", err)
	}
	member.configBody = config
	member.Target = parseTargetFromConfig(config)

	services, err := s.fetchServices(ctx, addr)
	if err != nil {
		return member, fmt.Errorf("fetching services: %w", err)
	}
	member.Services = services

	build, err := s.fetchBuild(ctx, addr)
	if err != nil {
		return member, fmt.Errorf("fetching build info: %w", err)
	}
	member.Build = build

	readyResp, err := s.checkNodeReadiness(ctx, addr)
	if err != nil {
		return member, fmt.Errorf("checking node readiness: %w", err)
	}
	member.Ready = readyResp

	return member, nil
}

func (s *Service) buildDownstreamPath(addr, endpoint string) string {
	//TODO(twhitney): protocol should be configurable here, or fetched from the ring
	return fmt.Sprintf("http://%s%s", addr, endpoint)
}

// readResponseError checks the HTTP response for errors and returns an appropriate error message.
// If the response status is not OK, it reads and includes the response body in the error message.
func readResponseError(resp *http.Response, operation string) error {
	if resp == nil {
		return fmt.Errorf("%s: no response received", operation)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("%s failed: %s, error reading body: %v", operation, resp.Status, err)
		}
		return fmt.Errorf("%s failed: %s, response: %s", operation, resp.Status, string(body))
	}
	return nil
}

// NodeDetails contains the details of a node in the cluster.
// It adds on top of Member the config, build, clusterID, clusterSeededAt, os, arch, edition and registered analytics metrics.
type NodeDetails struct {
	Member
	Config          string                 `json:"config"`
	ClusterID       string                 `json:"clusterID"`
	ClusterSeededAt int64                  `json:"clusterSeededAt"`
	OS              string                 `json:"os"`
	Arch            string                 `json:"arch"`
	Edition         string                 `json:"edition"`
	Metrics         map[string]interface{} `json:"metrics"`
}

func (s *Service) fetchDetails(ctx context.Context, nodeName string) (NodeDetails, error) {
	addr, err := s.findNodeAddressByName(nodeName)
	if err != nil {
		return NodeDetails{}, fmt.Errorf("fetching node address: %w", err)
	}

	report, err := s.fetchAnalytics(ctx, addr)
	if err != nil {
		return NodeDetails{}, fmt.Errorf("fetching analytics: %w", err)
	}

	member, err := s.fetchMemberState(ctx, addr)
	if err != nil {
		return NodeDetails{}, fmt.Errorf("fetching member state: %w", err)
	}

	return NodeDetails{
		Member:          member,
		Config:          member.configBody,
		Metrics:         report.Metrics,
		ClusterID:       report.ClusterID,
		ClusterSeededAt: report.CreatedAt.UnixMilli(),
		OS:              report.Os,
		Arch:            report.Arch,
		Edition:         report.Edition,
	}, nil
}

func (s *Service) fetchAnalytics(ctx context.Context, addr string) (analytics.Report, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.buildDownstreamPath(addr, "/analytics"), nil)
	if err != nil {
		return analytics.Report{}, fmt.Errorf("creating request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return analytics.Report{}, fmt.Errorf("sending request: %w", err)
	}

	if err := readResponseError(resp, "fetch analytics"); err != nil {
		return analytics.Report{}, err
	}
	defer resp.Body.Close()

	var report analytics.Report
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return analytics.Report{}, fmt.Errorf("decoding response: %w", err)
	}
	return report, nil
}

// fetchConfig retrieves the configuration of a cluster member by address.
func (s *Service) fetchConfig(ctx context.Context, addr string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.buildDownstreamPath(addr, "/config"), nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sending request: %w", err)
	}

	if err := readResponseError(resp, "fetch config"); err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}
	return string(body), nil
}

// fetchServices retrieves the service states of a cluster member by address.
func (s *Service) fetchServices(ctx context.Context, addr string) ([]ServiceState, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.buildDownstreamPath(addr, "/services"), nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	if err := readResponseError(resp, "fetch services"); err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	return parseServices(string(body))
}

// fetchBuild retrieves the build information of a cluster member by address.
func (s *Service) fetchBuild(ctx context.Context, addr string) (BuildInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.buildDownstreamPath(addr, "/loki/api/v1/status/buildinfo"), nil)
	if err != nil {
		return BuildInfo{}, fmt.Errorf("creating request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return BuildInfo{}, fmt.Errorf("sending request: %w", err)
	}

	if err := readResponseError(resp, "fetch build info"); err != nil {
		return BuildInfo{}, err
	}
	defer resp.Body.Close()

	var build BuildInfo
	if err := json.NewDecoder(resp.Body).Decode(&build); err != nil {
		return BuildInfo{}, fmt.Errorf("decoding response: %w", err)
	}
	return build, nil
}

type ReadyResponse struct {
	IsReady bool   `json:"isReady"`
	Message string `json:"message"`
}

func (s *Service) checkNodeReadiness(ctx context.Context, addr string) (ReadyResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.buildDownstreamPath(addr, "/ready"), nil)
	if err != nil {
		return ReadyResponse{}, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return ReadyResponse{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ReadyResponse{}, err
	}

	return ReadyResponse{
		IsReady: resp.StatusCode == http.StatusOK && strings.TrimSpace(string(body)) == "ready",
		Message: string(body),
	}, nil
}

// parseTargetFromConfig extracts the target value from a YAML configuration string.
// Returns "unknown" if the config cannot be parsed or the target is not found.
func parseTargetFromConfig(config string) string {
	var cfg map[string]interface{}
	if err := yaml.Unmarshal([]byte(config), &cfg); err != nil {
		return "unknown"
	}
	target, _ := cfg["target"].(string)
	return target
}

// parseServices parses a string containing service states in the format:
// service => status
// Returns a slice of ServiceState structs.
func parseServices(body string) ([]ServiceState, error) {
	var services []ServiceState
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		parts := strings.SplitN(line, " => ", 2)
		if len(parts) != 2 {
			continue
		}
		services = append(services, ServiceState{
			Service: parts[0],
			Status:  parts[1],
		})
	}
	return services, nil
}
