/*
 *
 * Copyright 2021 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package clusterresolver

import (
	"sync"

	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

// resourceUpdate is a combined update from all the resources, in the order of
// priority. For example, it can be {EDS, EDS, DNS}.
type resourceUpdate struct {
	priorities []priorityConfig
	err        error
}

type discoveryMechanism interface {
	lastUpdate() (interface{}, bool)
	resolveNow()
	stop()
}

// discoveryMechanismKey is {type+resource_name}, it's used as the map key, so
// that the same resource resolver can be reused (e.g. when there are two
// mechanisms, both for the same EDS resource, but has different circuit
// breaking config.
type discoveryMechanismKey struct {
	typ  DiscoveryMechanismType
	name string
}

// resolverMechanismTuple is needed to keep the resolver and the discovery
// mechanism together, because resolvers can be shared. And we need the
// mechanism for fields like circuit breaking, LRS etc when generating the
// balancer config.
type resolverMechanismTuple struct {
	dm    DiscoveryMechanism
	dmKey discoveryMechanismKey
	r     discoveryMechanism

	childNameGen *nameGenerator
}

type resourceResolver struct {
	parent        *clusterResolverBalancer
	updateChannel chan *resourceUpdate

	// mu protects the slice and map, and content of the resolvers in the slice.
	mu         sync.Mutex
	mechanisms []DiscoveryMechanism
	children   []resolverMechanismTuple
	// childrenMap's value only needs the resolver implementation (type
	// discoveryMechanism) and the childNameGen. The other two fields are not
	// used.
	//
	// TODO(cleanup): maybe we can make a new type with just the necessary
	// fields, and use it here instead.
	childrenMap map[discoveryMechanismKey]resolverMechanismTuple
	// Each new discovery mechanism needs a child name generator to reuse child
	// policy names. But to make sure the names across discover mechanism
	// doesn't conflict, we need a seq ID. This ID is incremented for each new
	// discover mechanism.
	childNameGeneratorSeqID uint64
}

func newResourceResolver(parent *clusterResolverBalancer) *resourceResolver {
	return &resourceResolver{
		parent:        parent,
		updateChannel: make(chan *resourceUpdate, 1),
		childrenMap:   make(map[discoveryMechanismKey]resolverMechanismTuple),
	}
}

func equalDiscoveryMechanisms(a, b []DiscoveryMechanism) bool {
	if len(a) != len(b) {
		return false
	}
	for i, aa := range a {
		bb := b[i]
		if !aa.Equal(bb) {
			return false
		}
	}
	return true
}

func (rr *resourceResolver) updateMechanisms(mechanisms []DiscoveryMechanism) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	if equalDiscoveryMechanisms(rr.mechanisms, mechanisms) {
		return
	}
	rr.mechanisms = mechanisms
	rr.children = make([]resolverMechanismTuple, len(mechanisms))
	newDMs := make(map[discoveryMechanismKey]bool)

	// Start one watch for each new discover mechanism {type+resource_name}.
	for i, dm := range mechanisms {
		switch dm.Type {
		case DiscoveryMechanismTypeEDS:
			// If EDSServiceName is not set, use the cluster name as EDS service
			// name to watch.
			nameToWatch := dm.EDSServiceName
			if nameToWatch == "" {
				nameToWatch = dm.Cluster
			}
			dmKey := discoveryMechanismKey{typ: dm.Type, name: nameToWatch}
			newDMs[dmKey] = true

			r, ok := rr.childrenMap[dmKey]
			if !ok {
				r = resolverMechanismTuple{
					dm:           dm,
					dmKey:        dmKey,
					r:            newEDSResolver(nameToWatch, rr.parent.xdsClient, rr),
					childNameGen: newNameGenerator(rr.childNameGeneratorSeqID),
				}
				rr.childrenMap[dmKey] = r
				rr.childNameGeneratorSeqID++
			} else {
				// If this is not new, keep the fields (especially
				// childNameGen), and only update the DiscoveryMechanism.
				//
				// Note that the same dmKey doesn't mean the same
				// DiscoveryMechanism. There are fields (e.g.
				// MaxConcurrentRequests) in DiscoveryMechanism that are not
				// copied to dmKey, we need to keep those updated.
				r.dm = dm
			}
			rr.children[i] = r
		case DiscoveryMechanismTypeLogicalDNS:
			// Name to resolve in DNS is the hostname, not the ClientConn
			// target.
			dmKey := discoveryMechanismKey{typ: dm.Type, name: dm.DNSHostname}
			newDMs[dmKey] = true

			r, ok := rr.childrenMap[dmKey]
			if !ok {
				r = resolverMechanismTuple{
					dm:           dm,
					dmKey:        dmKey,
					r:            newDNSResolver(dm.DNSHostname, rr),
					childNameGen: newNameGenerator(rr.childNameGeneratorSeqID),
				}
				rr.childrenMap[dmKey] = r
				rr.childNameGeneratorSeqID++
			} else {
				r.dm = dm
			}
			rr.children[i] = r
		}
	}
	// Stop the resources that were removed.
	for dm, r := range rr.childrenMap {
		if !newDMs[dm] {
			delete(rr.childrenMap, dm)
			r.r.stop()
		}
	}
	// Regenerate even if there's no change in discovery mechanism, in case
	// priority order changed.
	rr.generate()
}

// resolveNow is typically called to trigger re-resolve of DNS. The EDS
// resolveNow() is a noop.
func (rr *resourceResolver) resolveNow() {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	for _, r := range rr.childrenMap {
		r.r.resolveNow()
	}
}

func (rr *resourceResolver) stop() {
	rr.mu.Lock()
	// Save the previous childrenMap to stop the children outside the mutex,
	// and reinitialize the map.  We only need to reinitialize to allow for the
	// policy to be reused if the resource comes back.  In practice, this does
	// not happen as the parent LB policy will also be closed, causing this to
	// be removed entirely, but a future use case might want to reuse the
	// policy instead.
	cm := rr.childrenMap
	rr.childrenMap = make(map[discoveryMechanismKey]resolverMechanismTuple)
	rr.mechanisms = nil
	rr.children = nil
	rr.mu.Unlock()

	for _, r := range cm {
		r.r.stop()
	}
}

// generate collects all the updates from all the resolvers, and push the
// combined result into the update channel. It only pushes the update when all
// the child resolvers have received at least one update, otherwise it will
// wait.
//
// caller must hold rr.mu.
func (rr *resourceResolver) generate() {
	var ret []priorityConfig
	for _, rDM := range rr.children {
		u, ok := rDM.r.lastUpdate()
		if !ok {
			// Don't send updates to parent until all resolvers have update to
			// send.
			return
		}
		switch uu := u.(type) {
		case xdsresource.EndpointsUpdate:
			ret = append(ret, priorityConfig{mechanism: rDM.dm, edsResp: uu, childNameGen: rDM.childNameGen})
		case []string:
			ret = append(ret, priorityConfig{mechanism: rDM.dm, addresses: uu, childNameGen: rDM.childNameGen})
		}
	}
	select {
	case <-rr.updateChannel:
	default:
	}
	rr.updateChannel <- &resourceUpdate{priorities: ret}
}

type edsDiscoveryMechanism struct {
	cancel func()

	update         xdsresource.EndpointsUpdate
	updateReceived bool
}

func (er *edsDiscoveryMechanism) lastUpdate() (interface{}, bool) {
	if !er.updateReceived {
		return nil, false
	}
	return er.update, true
}

func (er *edsDiscoveryMechanism) resolveNow() {
}

func (er *edsDiscoveryMechanism) stop() {
	er.cancel()
}

// newEDSResolver starts the EDS watch on the given xds client.
func newEDSResolver(nameToWatch string, xdsc xdsclient.XDSClient, topLevelResolver *resourceResolver) *edsDiscoveryMechanism {
	ret := &edsDiscoveryMechanism{}
	topLevelResolver.parent.logger.Infof("EDS watch started on %v", nameToWatch)
	cancel := xdsc.WatchEndpoints(nameToWatch, func(update xdsresource.EndpointsUpdate, err error) {
		topLevelResolver.mu.Lock()
		defer topLevelResolver.mu.Unlock()
		if err != nil {
			select {
			case <-topLevelResolver.updateChannel:
			default:
			}
			topLevelResolver.updateChannel <- &resourceUpdate{err: err}
			return
		}
		ret.update = update
		ret.updateReceived = true
		topLevelResolver.generate()
	})
	ret.cancel = func() {
		topLevelResolver.parent.logger.Infof("EDS watch canceled on %v", nameToWatch)
		cancel()
	}
	return ret
}
