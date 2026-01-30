// Commands from https://redis.io/commands#cluster

package miniredis

import (
	"fmt"
	"strings"

	"github.com/alicebob/miniredis/v2/server"
)

// commandsCluster handles some cluster operations.
func commandsCluster(m *Miniredis) {
	m.srv.Register("CLUSTER", m.cmdCluster)
}

func (m *Miniredis) cmdCluster(c *server.Peer, cmd string, args []string) {
	if !m.handleAuth(c) {
		return
	}

	if len(args) < 1 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	switch strings.ToUpper(args[0]) {
	case "SLOTS":
		m.cmdClusterSlots(c, cmd, args)
	case "KEYSLOT":
		m.cmdClusterKeySlot(c, cmd, args)
	case "NODES":
		m.cmdClusterNodes(c, cmd, args)
	case "SHARDS":
		m.cmdClusterShards(c, cmd, args)
	default:
		setDirty(c)
		c.WriteError(fmt.Sprintf("ERR 'CLUSTER %s' not supported", strings.Join(args, " ")))
		return
	}
}

// CLUSTER SLOTS
func (m *Miniredis) cmdClusterSlots(c *server.Peer, cmd string, args []string) {
	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		c.WriteLen(1)
		c.WriteLen(3)
		c.WriteInt(0)
		c.WriteInt(16383)
		c.WriteLen(3)
		c.WriteBulk(m.srv.Addr().IP.String())
		c.WriteInt(m.srv.Addr().Port)
		c.WriteBulk("09dbe9720cda62f7865eabc5fd8857c5d2678366")
	})
}

// CLUSTER KEYSLOT
func (m *Miniredis) cmdClusterKeySlot(c *server.Peer, cmd string, args []string) {
	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		c.WriteInt(163)
	})
}

// CLUSTER NODES
func (m *Miniredis) cmdClusterNodes(c *server.Peer, cmd string, args []string) {
	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		// do not try to use m.Addr() here, as m is blocked by this tx.
		addr := m.srv.Addr()
		port := m.srv.Addr().Port
		c.WriteBulk(fmt.Sprintf("e7d1eecce10fd6bb5eb35b9f99a514335d9ba9ca %s@%d myself,master - 0 0 1 connected 0-16383", addr, port))
	})
}

// CLUSTER SHARDS
func (m *Miniredis) cmdClusterShards(c *server.Peer, cmd string, args []string) {
	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		addr := m.srv.Addr()
		host := addr.IP.String()
		port := addr.Port

		// Array of shards (we return 1 shard)
		c.WriteLen(1)

		// Shard is a map with 2 keys: "slots" and "nodes"
		c.WriteMapLen(2)

		// "slots": flat list of start/end pairs (inclusive ranges)
		c.WriteBulk("slots")
		c.WriteLen(2)
		c.WriteInt(0)
		c.WriteInt(16383)

		// "nodes": array of node maps
		c.WriteBulk("nodes")
		c.WriteLen(1)

		// Node map.
		// (id, endpoint, ip, port, role, replication-offset, health)
		c.WriteMapLen(6)

		c.WriteBulk("id")
		c.WriteBulk("13f84e686106847b76671957dd348fde540a77bb")

		//c.WriteBulk("endpoint")
		//c.WriteBulk(host) // or host:port if your client expects that

		c.WriteBulk("ip")
		c.WriteBulk(host)

		c.WriteBulk("port")
		c.WriteInt(port)

		c.WriteBulk("role")
		c.WriteBulk("master")

		c.WriteBulk("replication-offset")
		c.WriteInt(0)

		c.WriteBulk("health")
		c.WriteBulk("online")
	})
}
