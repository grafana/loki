package miniredis

import "github.com/alicebob/miniredis/v2/server"

// commandsHll handles all hll related operations.
func commandsHll(m *Miniredis) {
	m.srv.Register("PFADD", m.cmdPfadd)
	m.srv.Register("PFCOUNT", m.cmdPfcount, server.ReadOnlyOption())
	m.srv.Register("PFMERGE", m.cmdPfmerge)
}

// PFADD
func (m *Miniredis) cmdPfadd(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, atLeast(2)) {
		return
	}

	key, items := args[0], args[1:]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if db.exists(key) && db.t(key) != keyTypeHll {
			c.WriteError(ErrNotValidHllValue.Error())
			return
		}

		altered := db.hllAdd(key, items...)
		c.WriteInt(altered)
	})
}

// PFCOUNT
func (m *Miniredis) cmdPfcount(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, atLeast(1)) {
		return
	}

	keys := args

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		count, err := db.hllCount(keys)
		if err != nil {
			c.WriteError(err.Error())
			return
		}

		c.WriteInt(count)
	})
}

// PFMERGE
func (m *Miniredis) cmdPfmerge(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, atLeast(1)) {
		return
	}

	keys := args

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if err := db.hllMerge(keys); err != nil {
			c.WriteError(err.Error())
			return
		}
		c.WriteOK()
	})
}
