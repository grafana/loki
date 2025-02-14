package ckit

import (
	"sort"

	"github.com/grafana/ckit/peer"
)

// An Observer watches a Node, waiting for its peers to change.
type Observer interface {
	// NotifyPeersChanged is invoked any time the set of Peers for a node
	// changes. The slice of peers should not be modified.
	//
	// The real list of peers may have changed; call Node.Peers to get the
	// current list.
	//
	// If NotifyPeersChanged returns false, the Observer will no longer receive
	// any notifications. This can be used for single-use watches.
	NotifyPeersChanged(peers []peer.Peer) (reregister bool)
}

// FuncObserver implements Observer.
type FuncObserver func(peers []peer.Peer) (reregister bool)

// NotifyPeersChanged implements Observer.
func (f FuncObserver) NotifyPeersChanged(peers []peer.Peer) (reregister bool) { return f(peers) }

// ParticipantObserver wraps an observer and filters out events where the list
// of peers in the Participants state haven't changed. When the set of
// participants have changed, next.NotifyPeersChanged will be invoked with the
// full set of peers (i.e., not just participants).
func ParticipantObserver(next Observer) Observer {
	return &participantObserver{next: next}
}

type participantObserver struct {
	lastParticipants []peer.Peer // Participants ordered by name
	next             Observer
}

func (po *participantObserver) NotifyPeersChanged(peers []peer.Peer) (reregister bool) {
	// Filter peers down to those in StateParticipant.
	participants := make([]peer.Peer, 0, len(peers))
	for _, p := range peers {
		if p.State == peer.StateParticipant {
			participants = append(participants, p)
		}
	}
	sort.Slice(participants, func(i, j int) bool { return participants[i].Name < participants[j].Name })

	if peersEqual(participants, po.lastParticipants) {
		return true
	}

	po.lastParticipants = participants
	return po.next.NotifyPeersChanged(peers)
}

func peersEqual(a, b []peer.Peer) bool {
	if len(a) != len(b) {
		return false
	}

	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
