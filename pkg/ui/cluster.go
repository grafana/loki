package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/grafana/ckit/peer"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v3"
)

// Cluster represents a collection of cluster members.
type Cluster struct {
	Members map[string]Member `json:"members"`
}

// Member represents a node in the cluster with its current state and capabilities.
type Member struct {
	Addr     string         `json:"addr"`
	State    string         `json:"state"`
	IsSelf   bool           `json:"isSelf"`
	Target   string         `json:"target"`
	Services []ServiceState `json:"services"`
	Build    BuildInfo      `json:"build"`
	Error    error          `json:"error,omitempty"`
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

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(16)

	for _, p := range s.node.Peers() {
		peer := p // Create new variable to avoid closure issues
		g.Go(func() error {
			member, err := s.fetchMemberState(ctx, peer)
			if err != nil {
				member.Error = err
			}
			cluster.Members[peer.Name] = member
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return Cluster{}, fmt.Errorf("fetching cluster members: %w", err)
	}
	return cluster, nil
}

// fetchMemberState retrieves the complete state of a single cluster member.
func (s *Service) fetchMemberState(ctx context.Context, peer peer.Peer) (Member, error) {
	member := Member{
		Addr:   peer.Addr,
		IsSelf: peer.Self,
		State:  peer.State.String(),
	}

	config, err := s.fetchConfig(ctx, peer)
	if err != nil {
		return member, fmt.Errorf("fetching config: %w", err)
	}
	member.Target = parseTargetFromConfig(config)

	services, err := s.fetchServices(ctx, peer)
	if err != nil {
		return member, fmt.Errorf("fetching services: %w", err)
	}
	member.Services = services

	build, err := s.fetchBuild(ctx, peer)
	if err != nil {
		return member, fmt.Errorf("fetching build info: %w", err)
	}
	member.Build = build

	return member, nil
}

// buildProxyPath constructs the proxy URL path for a given peer and endpoint.
func (s *Service) buildProxyPath(peer peer.Peer, endpoint string) string {
	return fmt.Sprintf("http://%s/ui/api/v1/proxy/%s%s", s.localAddr, peer.Name, endpoint)
}

// readResponseError checks the HTTP response for errors and returns an appropriate error message.
// If the response status is not OK, it reads and includes the response body in the error message.
func readResponseError(resp *http.Response, operation string) error {
	if resp == nil {
		return fmt.Errorf("%s: no response received", operation)
	}
	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("%s failed: %s, error reading body: %v", operation, resp.Status, err)
		}
		return fmt.Errorf("%s failed: %s, response: %s", operation, resp.Status, string(body))
	}
	return nil
}

// fetchConfig retrieves the configuration of a cluster member.
func (s *Service) fetchConfig(ctx context.Context, peer peer.Peer) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.buildProxyPath(peer, "/config"), nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if err := readResponseError(resp, "fetch config"); err != nil {
		return "", err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}
	return string(body), nil
}

// fetchServices retrieves the service states of a cluster member.
func (s *Service) fetchServices(ctx context.Context, peer peer.Peer) ([]ServiceState, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.buildProxyPath(peer, "/services"), nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if err := readResponseError(resp, "fetch services"); err != nil {
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	return parseServices(string(body))
}

// fetchBuild retrieves the build information of a cluster member.
func (s *Service) fetchBuild(ctx context.Context, peer peer.Peer) (BuildInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.buildProxyPath(peer, "/loki/api/v1/status/buildinfo"), nil)
	if err != nil {
		return BuildInfo{}, fmt.Errorf("creating request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return BuildInfo{}, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if err := readResponseError(resp, "fetch build info"); err != nil {
		return BuildInfo{}, err
	}

	var build BuildInfo
	if err := json.NewDecoder(resp.Body).Decode(&build); err != nil {
		return BuildInfo{}, fmt.Errorf("decoding response: %w", err)
	}
	return build, nil
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
