package memberlist

import (
	"flag"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/hashicorp/memberlist"
)

// NodeRole represents the role of a node in the memberlist cluster.
type NodeRole uint8

const (
	// NodeRoleMember represents a standard member node.
	NodeRoleMember NodeRole = 1
	// NodeRoleBridge represents a bridge node that connects different zones.
	NodeRoleBridge NodeRole = 2
)

// String returns the string representation of the node role.
func (r NodeRole) String() string {
	switch r {
	case NodeRoleMember:
		return "member"
	case NodeRoleBridge:
		return "bridge"
	default:
		return fmt.Sprintf("unknown(%d)", r)
	}
}

const (
	// MaxZoneNameLength is the maximum zone name length (to keep metadata compact).
	MaxZoneNameLength = 16

	// Role configuration values.
	roleConfigMember = "member"
	roleConfigBridge = "bridge"
)

// ZoneAwareRoutingConfig holds configuration for zone-aware routing in memberlist.
type ZoneAwareRoutingConfig struct {
	Enabled bool   `yaml:"enabled" category:"experimental"`
	Zone    string `yaml:"instance_availability_zone" category:"experimental"`
	Role    string `yaml:"role" category:"experimental"`
}

// RegisterFlagsWithPrefix registers flags with the given prefix.
func (cfg *ZoneAwareRoutingConfig) RegisterFlagsWithPrefix(f *flag.FlagSet, prefix string) {
	f.BoolVar(&cfg.Enabled, prefix+"enabled", false, "Enable zone-aware routing for memberlist gossip.")
	f.StringVar(&cfg.Zone, prefix+"instance-availability-zone", "", "Availability zone where this node is running.")
	f.StringVar(&cfg.Role, prefix+"role", roleConfigMember, fmt.Sprintf("Role of this node in the cluster. Valid values: %s, %s.", roleConfigMember, roleConfigBridge))
}

// Validate validates the zone-aware routing configuration.
func (cfg *ZoneAwareRoutingConfig) Validate() error {
	// Only validate if enabled.
	if !cfg.Enabled {
		return nil
	}

	// Zone must be set.
	if cfg.Zone == "" {
		return fmt.Errorf("zone-aware routing is enabled but zone is not set")
	}

	// Zone length must not exceed maximum.
	if len(cfg.Zone) > MaxZoneNameLength {
		return fmt.Errorf("zone name too long: %d bytes (max %d)", len(cfg.Zone), MaxZoneNameLength)
	}

	// Role must be valid.
	if cfg.Role != NodeRoleMember.String() && cfg.Role != NodeRoleBridge.String() {
		return fmt.Errorf("invalid role: %s (valid values: %s, %s)", cfg.Role, NodeRoleMember.String(), NodeRoleBridge.String())
	}

	return nil
}

// zoneAwareNodeSelectionDelegate implements the memberlist.NodeSelectionDelegate interface
// to provide zone-aware routing for gossip, probing, and push/pull operations.
type zoneAwareNodeSelectionDelegate struct {
	localRole NodeRole
	localZone string
	logger    log.Logger
}

// newZoneAwareNodeSelectionDelegate creates a new zone-aware node selection delegate.
func newZoneAwareNodeSelectionDelegate(localRole NodeRole, localZone string, logger log.Logger) *zoneAwareNodeSelectionDelegate {
	return &zoneAwareNodeSelectionDelegate{
		localRole: localRole,
		localZone: localZone,
		logger:    logger,
	}
}

// SelectNode implements memberlist.NodeSelectionDelegate.
// It determines whether a remote node should be selected for gossip operations and whether it should be preferred.
func (d *zoneAwareNodeSelectionDelegate) SelectNode(node memberlist.Node) (selected, preferred bool) {
	// Decode remote node metadata.
	remoteMeta := EncodedNodeMetadata(node.Meta)
	remoteZone := remoteMeta.Zone()
	remoteRole := remoteMeta.Role()

	// If either the local zone or the remote zone are unknown, select the node but don't prefer it.
	// This prevents network partitioning: if every other memberlist node filters it out, then that
	// remote node would not receive updates and would get isolated.
	if d.localZone == "" || remoteZone == "" {
		return true, false
	}

	switch d.localRole {
	case NodeRoleMember:
		// Members only select nodes in the same zone.
		if remoteZone == d.localZone {
			return true, false
		}
		return false, false

	case NodeRoleBridge:
		// Bridges select nodes in the same zone + bridge nodes in other zones.
		if remoteZone == d.localZone {
			// Same zone: select but don't prefer.
			return true, false
		}
		// Different zone: only select if it's a bridge node, and prefer it.
		if remoteRole == NodeRoleBridge {
			return true, true
		}
		return false, false

	default:
		level.Warn(d.logger).Log("msg", "memberlist zone-aware routing is running with an unknown role", "role", d.localRole)

		// Unknown role: select but don't prefer (should never happen).
		return true, false
	}
}
