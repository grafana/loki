// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package memcache

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/thanos-io/thanos/pkg/runutil"
)

type clusterConfig struct {
	version int
	nodes   []node
}

type node struct {
	dns  string
	ip   string
	port int
}

type Resolver interface {
	Resolve(ctx context.Context, address string) (*clusterConfig, error)
}

type memcachedAutoDiscovery struct {
	dialTimeout time.Duration
}

func (s *memcachedAutoDiscovery) Resolve(ctx context.Context, address string) (config *clusterConfig, err error) {
	conn, err := net.DialTimeout("tcp", address, s.dialTimeout)
	if err != nil {
		return nil, err
	}
	defer runutil.CloseWithErrCapture(&err, conn, "closing connection")

	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	if _, err := fmt.Fprintf(rw, "config get cluster\n"); err != nil {
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		return nil, err
	}

	config, err = s.parseConfig(rw.Reader)
	if err != nil {
		return nil, err
	}

	return config, err
}

func (s *memcachedAutoDiscovery) parseConfig(reader *bufio.Reader) (*clusterConfig, error) {
	clusterConfig := new(clusterConfig)

	configMeta, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read config metadata: %s", err)
	}
	configMeta = strings.TrimSpace(configMeta)

	// First line should be "CONFIG cluster 0 [length-of-payload-]
	configMetaComponents := strings.Split(configMeta, " ")
	if len(configMetaComponents) != 4 {
		return nil, fmt.Errorf("expected 4 components in config metadata, and received %d, meta: %s", len(configMetaComponents), configMeta)
	}

	configSize, err := strconv.Atoi(configMetaComponents[3])
	if err != nil {
		return nil, fmt.Errorf("failed to parse config size from metadata: %s, error: %s", configMeta, err)
	}

	configVersion, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to find config version: %s", err)
	}
	clusterConfig.version, err = strconv.Atoi(strings.TrimSpace(configVersion))
	if err != nil {
		return nil, fmt.Errorf("failed to parser config version: %s", err)
	}

	nodes, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read nodes: %s", err)
	}

	if len(configVersion)+len(nodes) != configSize {
		return nil, fmt.Errorf("expected %d in config payload, but got %d instead.", configSize, len(configVersion)+len(nodes))
	}

	for _, host := range strings.Split(strings.TrimSpace(nodes), " ") {
		dnsIpPort := strings.Split(host, "|")
		if len(dnsIpPort) != 3 {
			return nil, fmt.Errorf("node not in expected format: %s", dnsIpPort)
		}
		port, err := strconv.Atoi(dnsIpPort[2])
		if err != nil {
			return nil, fmt.Errorf("failed to parse port: %s, err: %s", dnsIpPort, err)
		}
		clusterConfig.nodes = append(clusterConfig.nodes, node{dns: dnsIpPort[0], ip: dnsIpPort[1], port: port})
	}

	return clusterConfig, nil
}
