package miniredis

import (
	"fmt"
	"strings"

	"github.com/alicebob/miniredis/v2/server"
)

const (
	clientsSectionName    = "clients"
	clientsSectionContent = "# Clients\nconnected_clients:%d\r\n"

	statsSectionName    = "stats"
	statsSectionContent = "# Stats\ntotal_connections_received:%d\r\ntotal_commands_processed:%d\r\n"
)

// Command 'INFO' from https://redis.io/commands/info/
func (m *Miniredis) cmdInfo(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, between(0, 1)) {
		return
	}
	var result string
	if len(args) == 0 {
		result = fmt.Sprintf(clientsSectionContent, m.Server().ClientsLen()) + fmt.Sprintf(statsSectionContent, m.Server().TotalConnections(), m.Server().TotalCommands())
		c.WriteBulk(result)
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		switch section := strings.ToLower(args[0]); section {
		case clientsSectionName:
			result = fmt.Sprintf(clientsSectionContent, m.Server().ClientsLen())
		case statsSectionName:
			result = fmt.Sprintf(statsSectionContent, m.Server().TotalConnections(), m.Server().TotalCommands())
		default:
			setDirty(c)
			c.WriteError(fmt.Sprintf("section (%s) is not supported", section))
			return
		}
		c.WriteBulk(result)
	})
}
