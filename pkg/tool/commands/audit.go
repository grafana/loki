package commands

import (
	"context"

	"github.com/grafana/loki/v3/pkg/tool/audit"
	"gopkg.in/alecthomas/kingpin.v2"
)

// AuditIndexCommand validates an index by checking existing chunks.
type AuditCommand struct {
	tenant string

	period     string
	path       string
	workingDir string
}

func (a *AuditCommand) auditIndex(_ *kingpin.ParseContext) error {
	return audit.Run(context.Background(), a.path, a.period, a.tenant, a.workingDir)
}

func (a *AuditCommand) Register(app *kingpin.Application) {
	auditCmd := app.Command("audit", "Audit Loki state.")
	auditCmd.Flag("tenant", "Tenant that will be audited.").Default("").Envar("LOKI_TENANT").StringVar(&a.tenant)

	// Register audit commands.
	auditIndexCmd := auditCmd.
		Command("index", "Audit the given index by checking all its expected chunks are present.").
		Action(a.auditIndex)

	// Audit index command.
	auditIndexCmd.Arg("period", "Index period that will be audited.").Required().StringVar(&a.period)
	auditIndexCmd.Arg("path", "TODO.").Required().StringVar(&a.path)
	auditIndexCmd.Arg("working-dir", "TODO.").Required().StringVar(&a.workingDir)
}
