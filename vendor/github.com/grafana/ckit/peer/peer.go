// Package peer describes a ckit peer.
package peer

import "encoding/json"

// Peer is a discovered node within the cluster.
type Peer struct {
	Name  string // Name of the Peer. Unique across the cluster.
	Addr  string // host:port address of the peer.
	Self  bool   // True if Peer is the local Node.
	State State  // State of the peer.
}

// String returns the name of p.
func (p Peer) String() string { return p.Name }

// MarshalJSON implements [json.Marshaler].
func (p Peer) MarshalJSON() ([]byte, error) {
	type peerStatusJSON struct {
		Name  string `json:"name"`
		Addr  string `json:"addr"`
		Self  bool   `json:"isSelf"`
		State string `json:"state"`
	}
	return json.Marshal(&peerStatusJSON{
		Name:  p.Name,
		Addr:  p.Addr,
		Self:  p.Self,
		State: p.State.String(),
	})
}

// UnmarshalJSON implements [json.Unmarshaler].
func (p *Peer) UnmarshalJSON(b []byte) error {
	type peerStatusJSON struct {
		Name  string `json:"name"`
		Addr  string `json:"addr"`
		Self  bool   `json:"isSelf"`
		State string `json:"state"`
	}

	var psj peerStatusJSON

	if err := json.Unmarshal(b, &psj); err != nil {
		return err
	}
	state, err := toState(psj.State)
	if err != nil {
		return err
	}

	p.Name = psj.Name
	p.Addr = psj.Addr
	p.Self = psj.Self
	p.State = state

	return nil
}
