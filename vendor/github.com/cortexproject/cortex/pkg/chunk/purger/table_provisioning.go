package purger

import (
	"flag"

	"github.com/cortexproject/cortex/pkg/chunk"
)

// TableProvisioningConfig holds config for table throuput and autoscaling. Currently only used by DynamoDB.
type TableProvisioningConfig struct {
	chunk.ActiveTableProvisionConfig `yaml:",inline"`
	TableTags                        chunk.Tags `yaml:"tags"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
// Adding a separate RegisterFlags here instead of using it from embedded chunk.ActiveTableProvisionConfig to be able to manage defaults separately.
// Defaults for WriteScale and ReadScale are shared for now to avoid adding further complexity since autoscaling is disabled anyways by default.
func (cfg *TableProvisioningConfig) RegisterFlags(argPrefix string, f *flag.FlagSet) {
	// default values ActiveTableProvisionConfig
	cfg.ProvisionedWriteThroughput = 1
	cfg.ProvisionedReadThroughput = 300
	cfg.ProvisionedThroughputOnDemandMode = false

	cfg.ActiveTableProvisionConfig.RegisterFlags(argPrefix, f)
	f.Var(&cfg.TableTags, argPrefix+".tags", "Tag (of the form key=value) to be added to the tables. Supported by DynamoDB")
}

func (cfg DeleteStoreConfig) GetTables() []chunk.TableDesc {
	return []chunk.TableDesc{cfg.ProvisionConfig.BuildTableDesc(cfg.RequestsTableName, cfg.ProvisionConfig.TableTags)}
}
