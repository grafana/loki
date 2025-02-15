package gossiphttp

import "net"

type unknownAddr struct{}

var _ net.Addr = unknownAddr{}

func (unknownAddr) Network() string { return "tcp" }
func (unknownAddr) String() string  { return "unknown" }
