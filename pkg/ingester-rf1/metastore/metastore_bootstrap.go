package metastore

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/dns"
	"github.com/hashicorp/raft"
)

func (m *Metastore) bootstrap() error {
	peers, err := m.bootstrapPeers()
	if err != nil {
		return fmt.Errorf("failed to resolve peers: %w", err)
	}
	logger := log.With(m.logger,
		"server_id", m.config.Raft.ServerID,
		"advertise_address", m.config.Raft.AdvertiseAddress,
		"peers", fmt.Sprint(peers))
	if raft.ServerAddress(m.config.Raft.AdvertiseAddress) != peers[0].Address {
		_ = level.Info(logger).Log("msg", "not the bootstrap node, skipping")
		return nil
	}
	_ = level.Info(logger).Log("msg", "bootstrapping raft")
	bootstrap := m.raft.BootstrapCluster(raft.Configuration{Servers: peers})
	if bootstrapErr := bootstrap.Error(); bootstrapErr != nil {
		if !errors.Is(bootstrapErr, raft.ErrCantBootstrap) {
			return fmt.Errorf("failed to bootstrap raft: %w", bootstrapErr)
		}
	}
	return nil
}

func (m *Metastore) bootstrapPeers() ([]raft.Server, error) {
	// The peer list always includes the local node.
	peers := make([]raft.Server, 0, len(m.config.Raft.BootstrapPeers)+1)
	peers = append(peers, raft.Server{
		Suffrage: raft.Voter,
		ID:       raft.ServerID(m.config.Raft.ServerID),
		Address:  raft.ServerAddress(m.config.Raft.AdvertiseAddress),
	})
	// Note that raft requires stable node IDs, therefore we're using
	// the node FQDN:port for both purposes: as the identifier and as the
	// address. This requires a DNS SRV record lookup without further
	// resolution of A records (dnssrvnoa+).
	//
	// Alternatively, peers may be specified explicitly in the
	// "{addr}</{node_id}>" format, where the node is the optional node
	// identifier.
	var resolve []string
	for _, peer := range m.config.Raft.BootstrapPeers {
		if strings.Contains(peer, "+") {
			resolve = append(resolve, peer)
		} else {
			peers = append(peers, parsePeer(peer))
		}
	}
	if len(resolve) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		prov := dns.NewProvider(m.logger, m.reg, dns.MiekgdnsResolverType)
		if err := prov.Resolve(ctx, resolve); err != nil {
			return nil, fmt.Errorf("failed to resolve bootstrap peers: %w", err)
		}
		resolvedPeers := prov.Addresses()
		if len(resolvedPeers) == 0 {
			// The local node is the only one in the cluster, but peers
			// were supposed to be present. Stop here to avoid bootstrapping
			// a single-node cluster.
			return nil, fmt.Errorf("bootstrap peers can't be resolved")
		}
		for _, peer := range resolvedPeers {
			peers = append(peers, raft.Server{
				Suffrage: raft.Voter,
				ID:       raft.ServerID(peer),
				Address:  raft.ServerAddress(peer),
			})
		}
	}
	// Finally, we sort and deduplicate the peers: the first one
	// is to boostrap the cluster. If there are nodes with distinct
	// IDs but the same address, bootstrapping will fail.
	slices.SortFunc(peers, func(a, b raft.Server) int {
		return strings.Compare(string(a.ID), string(b.ID))
	})
	peers = slices.CompactFunc(peers, func(a, b raft.Server) bool {
		return a.ID == b.ID
	})
	return peers, nil
}

func parsePeer(raw string) raft.Server {
	// The string may be "{addr}" or "{addr}/{node_id}".
	parts := strings.SplitN(raw, "/", 2)
	var addr string
	var node string
	if len(parts) == 2 {
		addr = parts[0]
		node = parts[1]
	} else {
		addr = raw
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// No port specified.
		host = addr
	}
	if node == "" {
		// No node_id specified.
		node = host
	}
	return raft.Server{
		Suffrage: raft.Voter,
		ID:       raft.ServerID(node),
		Address:  raft.ServerAddress(addr),
	}
}
