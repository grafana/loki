package ui

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"strconv"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

func (s *Service) getBootstrapPeers() ([]string, error) {
	peers, err := s.discoverPeers()
	if err != nil {
		return nil, err
	}
	return selectRandomPeers(peers, s.cfg.ClusterMaxJoinPeers), nil
}

func selectRandomPeers(peers []string, maxPeers int) []string {
	// Here we return the entire list because we can't take a subset.
	if maxPeers == 0 || len(peers) < maxPeers {
		return peers
	}

	// We shuffle the list and return only a subset of the peers.
	rand.Shuffle(len(peers), func(i, j int) {
		peers[i], peers[j] = peers[j], peers[i]
	})
	return peers[:maxPeers]
}

func (s *Service) discoverNewPeers(prevPeers map[string]struct{}) ([]string, error) {
	peers, err := s.discoverPeers()
	if err != nil {
		return nil, err
	}

	// Build list of new peers that weren't in previous list
	var newPeers []string
	for _, peer := range peers {
		if _, ok := prevPeers[peer]; !ok {
			newPeers = append(newPeers, peer)
			prevPeers[peer] = struct{}{}
		}
	}

	return selectRandomPeers(newPeers, s.cfg.ClusterMaxJoinPeers), nil
}

func (s *Service) discoverPeers() ([]string, error) {
	if len(s.cfg.Discovery.JoinPeers) == 0 {
		return nil, nil
	}
	// Use these resolvers in order to resolve the provided addresses into a form that can be used by clustering.
	resolvers := []addressResolver{
		ipResolver(),
		dnsAResolver(nil),
		dnsSRVResolver(nil),
	}

	// Get the addresses.
	addresses, err := buildJoinAddresses(s.cfg, resolvers, s.logger)
	if err != nil {
		return nil, fmt.Errorf("static peer discovery: %w", err)
	}

	// Return unique addresses.
	return uniq(addresses), nil
}

func uniq(addresses []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, addr := range addresses {
		if !seen[addr] {
			seen[addr] = true
			result = append(result, addr)
		}
	}
	return result
}

func buildJoinAddresses(opts Config, resolvers []addressResolver, logger log.Logger) ([]string, error) {
	var (
		result      []string
		deferredErr error
	)

	for _, addr := range opts.Discovery.JoinPeers {
		// See if we have a port override, if not use the default port.
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
			port = strconv.Itoa(opts.AdvertisePort)
		}

		atLeastOneSuccess := false
		for _, resolver := range resolvers {
			resolved, err := resolver(host)
			deferredErr = errors.Join(deferredErr, err)
			for _, foundAddr := range resolved {
				result = append(result, net.JoinHostPort(foundAddr, port))
			}
			// we stop once we find a resolver that succeeded for given address
			if len(resolved) > 0 {
				atLeastOneSuccess = true
				break
			}
		}

		if !atLeastOneSuccess {
			// It is still useful to know if user provided an address that we could not resolve, even
			// if another addresses resolve successfully, and we don't return an error. To keep things simple, we're
			// not including more detail as it's available through debug level.
			level.Warn(logger).Log("msg", "failed to resolve provided join address", "addr", addr)
		}
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("failed to find any valid join addresses: %w", deferredErr)
	}
	return result, nil
}

type addressResolver func(addr string) ([]string, error)

func ipResolver() addressResolver {
	return func(addr string) ([]string, error) {
		// Check if it's IP and use it if so.
		ip := net.ParseIP(addr)
		if ip == nil {
			return nil, fmt.Errorf("could not parse as an IP or IP:port address: %q", addr)
		}
		return []string{ip.String()}, nil
	}
}

func dnsAResolver(ipLookup func(string) ([]net.IP, error)) addressResolver {
	// Default to net.LookupIP if not provided. By default, this will look up A/AAAA records.
	if ipLookup == nil {
		ipLookup = net.LookupIP
	}
	return func(addr string) ([]string, error) {
		ips, err := ipLookup(addr)
		result := make([]string, 0, len(ips))
		for _, ip := range ips {
			result = append(result, ip.String())
		}
		if err != nil {
			err = fmt.Errorf("failed to resolve %q records: %w", "A/AAAA", err)
		}
		return result, err
	}
}

func dnsSRVResolver(srvLookup func(service, proto, name string) (string, []*net.SRV, error)) addressResolver {
	// Default to net.LookupSRV if not provided.
	if srvLookup == nil {
		srvLookup = net.LookupSRV
	}
	return func(addr string) ([]string, error) {
		_, addresses, err := srvLookup("", "", addr)
		result := make([]string, 0, len(addresses))
		for _, a := range addresses {
			result = append(result, a.Target)
		}
		if err != nil {
			err = fmt.Errorf("failed to resolve %q records: %w", "SRV", err)
		}
		return result, err
	}
}
