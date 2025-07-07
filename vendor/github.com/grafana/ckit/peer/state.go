package peer

import "fmt"

// State is used by Nodes to inform their peers what their role is as part of
// gossip.
type State uint

const (
	// StateViewer is the default state. Nodes in the Viewer state have a
	// read-only view of the cluster, and are never considered owners when
	// hashing.
	StateViewer State = iota

	// StateParticipant marks a node as available to receive writes. It will be
	// considered a potential owner while hashing read or write operations.
	StateParticipant

	// StateTerminating is used when a Participant node is shutting down.
	// Terminating nodes are considered potential owners while hashing read
	// operations.
	StateTerminating
)

// AllStates holds a list of all valid states.
var AllStates = [...]State{
	StateViewer,
	StateParticipant,
	StateTerminating,
}

// String returns the string representation of s.
func (s State) String() string {
	switch s {
	case StateViewer:
		return "viewer"
	case StateParticipant:
		return "participant"
	case StateTerminating:
		return "terminating"
	default:
		return fmt.Sprintf("<unknown state %d>", s)
	}
}

func toState(s string) (State, error) {
	switch s {
	case "viewer":
		return StateViewer, nil
	case "participant":
		return StateParticipant, nil
	case "terminating":
		return StateTerminating, nil
	}

	return 0, fmt.Errorf("unknown state %q", s)
}
