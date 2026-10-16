package commands

import (
	"context"
	"flag"
	"fmt"

	"github.com/alecthomas/kingpin/v2"
	log "github.com/sirupsen/logrus"

	"github.com/grafana/loki/v3/pkg/lokitool/audit"
	util_cfg "github.com/grafana/loki/v3/pkg/util/cfg"
)

// AuditCommand validates an index by checking existing chunks.
type AuditCommand struct {
	configFile string
	indexFile  string
	period     string
	extraArgs  []string
}

// Register the audit commands and flags with the kingpin application.
func (a *AuditCommand) Register(app *kingpin.Application) {
	auditCmd := app.Command("audit", "Audit Loki state.")

	auditIndexCmd := auditCmd.
		Command("index", "Audit the given index by checking all its expected chunks are present.").
		Action(a.auditIndex)

	auditIndexCmd.Flag("config.file", "Auditing and storage configuration").Required().StringVar(&a.configFile)
	auditIndexCmd.Flag("index.file", "Index to be audited").Required().StringVar(&a.indexFile)
	auditIndexCmd.Flag("period", "Period of the index table, the number at the end of the table name, for example 19959. Takes precedence over the period of the configuration file.").StringVar(&a.period)
	auditIndexCmd.Arg("args", "Additional configuration flags in the syntax of the Go flag package, for example -tenant=12345. Separate them from the other flags with --.").StringsVar(&a.extraArgs)
}

// config returns the audit configuration from the configuration file and the flags.
func (a *AuditCommand) config() (audit.Config, error) {
	args := append([]string{"-config.file=" + a.configFile}, a.extraArgs...)
	if a.period != "" {
		args = append(args, "-period="+a.period)
	}

	var cfg audit.Config
	// The flag set only collects the flags of the audit configuration, it must not be shared between runs.
	if err := util_cfg.DefaultUnmarshal(&cfg, args, flag.NewFlagSet("audit", flag.ContinueOnError)); err != nil {
		return cfg, fmt.Errorf("failed parsing config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("failed validating config: %w", err)
	}
	return cfg, nil
}

func (a *AuditCommand) auditIndex(_ *kingpin.ParseContext) error {
	cfg, err := a.config()
	if err != nil {
		return err
	}

	found, missing, err := audit.Run(context.Background(), a.indexFile, cfg.Period, cfg, newKitLogger())
	if err != nil {
		return err
	}

	log.WithFields(log.Fields{"chunks_found": found, "chunks_missing": missing}).Info("finished auditing index")
	if missing > 0 {
		log.WithField("index", a.indexFile).Error("your index is missing chunks, please check previous logs for more information")
	} else {
		log.WithField("index", a.indexFile).Info("your index is healthy")
	}
	return nil
}
