package chunk

import "flag"

// ProvisionConfig holds config for provisioning capacity for index and chunk tables (on DynamoDB for now)
type ProvisionConfig struct {
	ActiveTableProvisionConfig   `yaml:",inline"`
	InactiveTableProvisionConfig `yaml:",inline"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *ProvisionConfig) RegisterFlags(argPrefix string, f *flag.FlagSet) {
	// defaults for ActiveTableProvisionConfig
	cfg.ProvisionedWriteThroughput = 1000
	cfg.ProvisionedReadThroughput = 300
	cfg.ProvisionedThroughputOnDemandMode = false

	cfg.ActiveTableProvisionConfig.RegisterFlags(argPrefix, f)
	cfg.InactiveTableProvisionConfig.RegisterFlags(argPrefix, f)
}

type ActiveTableProvisionConfig struct {
	ProvisionedThroughputOnDemandMode bool  `yaml:"enable_ondemand_throughput_mode"`
	ProvisionedWriteThroughput        int64 `yaml:"provisioned_write_throughput"`
	ProvisionedReadThroughput         int64 `yaml:"provisioned_read_throughput"`

	WriteScale AutoScalingConfig `yaml:"write_scale"`
	ReadScale  AutoScalingConfig `yaml:"read_scale"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
// Make sure defaults are set in the respective fields before calling RegisterFlags.
func (cfg *ActiveTableProvisionConfig) RegisterFlags(argPrefix string, f *flag.FlagSet) {
	f.Int64Var(&cfg.ProvisionedWriteThroughput, argPrefix+".write-throughput", cfg.ProvisionedWriteThroughput, "Table default write throughput. Supported by DynamoDB")
	f.Int64Var(&cfg.ProvisionedReadThroughput, argPrefix+".read-throughput", cfg.ProvisionedReadThroughput, "Table default read throughput. Supported by DynamoDB")
	f.BoolVar(&cfg.ProvisionedThroughputOnDemandMode, argPrefix+".enable-ondemand-throughput-mode", cfg.ProvisionedThroughputOnDemandMode, "Enables on demand throughput provisioning for the storage provider (if supported). Applies only to tables which are not autoscaled. Supported by DynamoDB")

	cfg.WriteScale.RegisterFlags(argPrefix+".write-throughput.scale", f)
	cfg.ReadScale.RegisterFlags(argPrefix+".read-throughput.scale", f)
}

type InactiveTableProvisionConfig struct {
	InactiveThroughputOnDemandMode bool  `yaml:"enable_inactive_throughput_on_demand_mode"`
	InactiveWriteThroughput        int64 `yaml:"inactive_write_throughput"`
	InactiveReadThroughput         int64 `yaml:"inactive_read_throughput"`

	InactiveWriteScale AutoScalingConfig `yaml:"inactive_write_scale"`
	InactiveReadScale  AutoScalingConfig `yaml:"inactive_read_scale"`

	InactiveWriteScaleLastN int64 `yaml:"inactive_write_scale_lastn"`
	InactiveReadScaleLastN  int64 `yaml:"inactive_read_scale_lastn"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *InactiveTableProvisionConfig) RegisterFlags(argPrefix string, f *flag.FlagSet) {
	f.Int64Var(&cfg.InactiveWriteThroughput, argPrefix+".inactive-write-throughput", 1, "Table write throughput for inactive tables. Supported by DynamoDB")
	f.Int64Var(&cfg.InactiveReadThroughput, argPrefix+".inactive-read-throughput", 300, "Table read throughput for inactive tables. Supported by DynamoDB")
	f.BoolVar(&cfg.InactiveThroughputOnDemandMode, argPrefix+".inactive-enable-ondemand-throughput-mode", false, "Enables on demand throughput provisioning for the storage provider (if supported). Applies only to tables which are not autoscaled. Supported by DynamoDB")

	cfg.InactiveWriteScale.RegisterFlags(argPrefix+".inactive-write-throughput.scale", f)
	cfg.InactiveReadScale.RegisterFlags(argPrefix+".inactive-read-throughput.scale", f)

	f.Int64Var(&cfg.InactiveWriteScaleLastN, argPrefix+".inactive-write-throughput.scale-last-n", 4, "Number of last inactive tables to enable write autoscale.")
	f.Int64Var(&cfg.InactiveReadScaleLastN, argPrefix+".inactive-read-throughput.scale-last-n", 4, "Number of last inactive tables to enable read autoscale.")
}

func (cfg ActiveTableProvisionConfig) BuildTableDesc(tableName string, tags Tags) TableDesc {
	table := TableDesc{
		Name:              tableName,
		ProvisionedRead:   cfg.ProvisionedReadThroughput,
		ProvisionedWrite:  cfg.ProvisionedWriteThroughput,
		UseOnDemandIOMode: cfg.ProvisionedThroughputOnDemandMode,
		Tags:              tags,
	}

	if cfg.WriteScale.Enabled {
		table.WriteScale = cfg.WriteScale
		table.UseOnDemandIOMode = false
	}

	if cfg.ReadScale.Enabled {
		table.ReadScale = cfg.ReadScale
		table.UseOnDemandIOMode = false
	}

	return table
}

func (cfg InactiveTableProvisionConfig) BuildTableDesc(tableName string, tags Tags, disableAutoscale bool) TableDesc {
	table := TableDesc{
		Name:              tableName,
		ProvisionedRead:   cfg.InactiveReadThroughput,
		ProvisionedWrite:  cfg.InactiveWriteThroughput,
		UseOnDemandIOMode: cfg.InactiveThroughputOnDemandMode,
		Tags:              tags,
	}

	if !disableAutoscale {
		if cfg.InactiveWriteScale.Enabled {
			table.WriteScale = cfg.InactiveWriteScale
			table.UseOnDemandIOMode = false
		}

		if cfg.InactiveReadScale.Enabled {
			table.ReadScale = cfg.InactiveReadScale
			table.UseOnDemandIOMode = false
		}
	}

	return table
}
