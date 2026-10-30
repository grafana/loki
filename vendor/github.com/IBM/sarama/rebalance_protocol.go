package sarama

import "fmt"

// RebalanceProtocol identifies a rebalance protocol supported by a BalanceStrategy
//
// The zero value is RebalanceProtocolEager, so a strategy that does not declare
// its supported protocols is treated as eager-only.
type RebalanceProtocol int8

const (
	// RebalanceProtocolEager revokes every assignment before a group rejoins
	RebalanceProtocolEager RebalanceProtocol = iota

	// RebalanceProtocolCooperative retains partitions that do not move during a rebalance
	RebalanceProtocolCooperative
)

func (p RebalanceProtocol) String() string {
	switch p {
	case RebalanceProtocolEager:
		return "eager"
	case RebalanceProtocolCooperative:
		return "cooperative"
	default:
		return fmt.Sprintf("unknown(%d)", int8(p))
	}
}

// RebalanceProtocolBalanceStrategy is an optional extension of BalanceStrategy
// that declares which rebalance protocols the strategy can take part in. A
// strategy that does not implement it is treated as eager-only.
//
// The consumer group uses the most preferred protocol common to every
// configured strategy. This allows cooperative strategies to be introduced
// alongside eager strategies during a rolling upgrade.
type RebalanceProtocolBalanceStrategy interface {
	BalanceStrategy

	SupportedProtocols() []RebalanceProtocol
}

func supportedProtocols(strategy BalanceStrategy) []RebalanceProtocol {
	if s, ok := strategy.(RebalanceProtocolBalanceStrategy); ok {
		return s.SupportedProtocols()
	}
	return []RebalanceProtocol{RebalanceProtocolEager}
}

func selectRebalanceProtocol(strategies []BalanceStrategy) (RebalanceProtocol, error) {
	if len(strategies) == 0 {
		return RebalanceProtocolEager, ConfigurationError("no Consumer.Group.Rebalance balance strategies configured")
	}

	allEager := true
	allCooperative := true
	for _, strategy := range strategies {
		supportsEager := false
		supportsCooperative := false
		for _, protocol := range supportedProtocols(strategy) {
			switch protocol {
			case RebalanceProtocolEager:
				supportsEager = true
			case RebalanceProtocolCooperative:
				supportsCooperative = true
			default:
				return RebalanceProtocolEager, ConfigurationError(fmt.Sprintf(
					"balance strategy %q declares unsupported rebalance protocol %s",
					strategy.Name(), protocol,
				))
			}
		}
		allEager = allEager && supportsEager
		allCooperative = allCooperative && supportsCooperative
	}

	if allCooperative {
		return RebalanceProtocolCooperative, nil
	}
	if allEager {
		return RebalanceProtocolEager, nil
	}
	return RebalanceProtocolEager, ConfigurationError("Consumer.Group.Rebalance.GroupStrategies do not have a commonly supported rebalance protocol")
}
