package maxminddb

import (
	"fmt"
	"net"
)

// Internal structure used to keep track of nodes we still need to visit.
type netNode struct {
	ip      net.IP
	bit     uint
	pointer uint
}

// Networks represents a set of subnets that we are iterating over.
type Networks struct {
	reader   *Reader
	nodes    []netNode // Nodes we still have to visit.
	lastNode netNode
	err      error

	skipAliasedNetworks bool
}

var (
	allIPv4 = &net.IPNet{IP: make(net.IP, 4), Mask: net.CIDRMask(0, 32)}
	allIPv6 = &net.IPNet{IP: make(net.IP, 16), Mask: net.CIDRMask(0, 128)}
)

// NetworksOption are options for Networks and NetworksWithin.
type NetworksOption func(*Networks)

// SkipAliasedNetworks is an option for Networks and NetworksWithin that
// makes them not iterate over aliases of the IPv4 subtree in an IPv6
// database, e.g., ::ffff:0:0/96, 2001::/32, and 2002::/16.
//
// You most likely want to set this. The only reason it isn't the default
// behavior is to provide backwards compatibility to existing users.
func SkipAliasedNetworks(networks *Networks) {
	networks.skipAliasedNetworks = true
}

// Networks returns an iterator that can be used to traverse all networks in
// the database.
//
// Please note that a MaxMind DB may map IPv4 networks into several locations
// in an IPv6 database. This iterator will iterate over all of these locations
// separately. To only iterate over the IPv4 networks once, use the
// SkipAliasedNetworks option.
func (r *Reader) Networks(options ...NetworksOption) *Networks {
	var networks *Networks
	if r.Metadata.IPVersion == 6 {
		networks = r.NetworksWithin(allIPv6, options...)
	} else {
		networks = r.NetworksWithin(allIPv4, options...)
	}

	return networks
}

// NetworksWithin returns an iterator that can be used to traverse all networks
// in the database which are contained in a given network.
//
// Please note that a MaxMind DB may map IPv4 networks into several locations
// in an IPv6 database. This iterator will iterate over all of these locations
// separately. To only iterate over the IPv4 networks once, use the
// SkipAliasedNetworks option.
//
// If the provided network is contained within a network in the database, the
// iterator will iterate over exactly one network, the containing network.
func (r *Reader) NetworksWithin(network *net.IPNet, options ...NetworksOption) *Networks {
	if r.Metadata.IPVersion == 4 && network.IP.To4() == nil {
		return &Networks{
			err: fmt.Errorf(
				"error getting networks with '%s': you attempted to use an IPv6 network in an IPv4-only database",
				network.String(),
			),
		}
	}

	networks := &Networks{reader: r}
	for _, option := range options {
		option(networks)
	}

	ip := network.IP
	prefixLength, _ := network.Mask.Size()

	if r.Metadata.IPVersion == 6 && len(ip) == net.IPv4len {
		if networks.skipAliasedNetworks {
			ip = net.IP{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, ip[0], ip[1], ip[2], ip[3]}
		} else {
			ip = ip.To16()
		}
		prefixLength += 96
	}

	pointer, bit := r.traverseTree(ip, 0, uint(prefixLength))
	networks.nodes = []netNode{
		{
			ip:      ip,
			bit:     uint(bit),
			pointer: pointer,
		},
	}

	return networks
}

// Next prepares the next network for reading with the Network method. It
// returns true if there is another network to be processed and false if there
// are no more networks or if there is an error.
func (n *Networks) Next() bool {
	if n.err != nil {
		return false
	}
	for len(n.nodes) > 0 {
		node := n.nodes[len(n.nodes)-1]
		n.nodes = n.nodes[:len(n.nodes)-1]

		for node.pointer != n.reader.Metadata.NodeCount {
			// This skips IPv4 aliases without hardcoding the networks that the writer
			// currently aliases.
			if n.skipAliasedNetworks && n.reader.ipv4Start != 0 &&
				node.pointer == n.reader.ipv4Start && !isInIPv4Subtree(node.ip) {
				break
			}

			if node.pointer > n.reader.Metadata.NodeCount {
				n.lastNode = node
				return true
			}
			ipRight := make(net.IP, len(node.ip))
			copy(ipRight, node.ip)
			if len(ipRight) <= int(node.bit>>3) {
				n.err = newInvalidDatabaseError(
					"invalid search tree at %v/%v", ipRight, node.bit)
				return false
			}
			ipRight[node.bit>>3] |= 1 << (7 - (node.bit % 8))

			offset := node.pointer * n.reader.nodeOffsetMult
			rightPointer := n.reader.nodeReader.readRight(offset)

			node.bit++
			n.nodes = append(n.nodes, netNode{
				pointer: rightPointer,
				ip:      ipRight,
				bit:     node.bit,
			})

			node.pointer = n.reader.nodeReader.readLeft(offset)
		}
	}

	return false
}

// Network returns the current network or an error if there is a problem
// decoding the data for the network. It takes a pointer to a result value to
// decode the network's data into.
func (n *Networks) Network(result interface{}) (*net.IPNet, error) {
	if n.err != nil {
		return nil, n.err
	}
	if err := n.reader.retrieveData(n.lastNode.pointer, result); err != nil {
		return nil, err
	}

	ip := n.lastNode.ip
	prefixLength := int(n.lastNode.bit)

	// We do this because uses of SkipAliasedNetworks expect the IPv4 networks
	// to be returned as IPv4 networks. If we are not skipping aliased
	// networks, then the user will get IPv4 networks from the ::FFFF:0:0/96
	// network as Go automatically converts those.
	if n.skipAliasedNetworks && isInIPv4Subtree(ip) {
		ip = ip[12:]
		prefixLength -= 96
	}

	return &net.IPNet{
		IP:   ip,
		Mask: net.CIDRMask(prefixLength, len(ip)*8),
	}, nil
}

// Err returns an error, if any, that was encountered during iteration.
func (n *Networks) Err() error {
	return n.err
}

// isInIPv4Subtree returns true if the IP is an IPv6 address in the database's
// IPv4 subtree.
func isInIPv4Subtree(ip net.IP) bool {
	if len(ip) != 16 {
		return false
	}
	for i := 0; i < 12; i++ {
		if ip[i] != 0 {
			return false
		}
	}
	return true
}
