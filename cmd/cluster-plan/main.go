package main

import (
	"errors"
	"flag"
	"fmt"
	"log"

	"github.com/grafana/loki/pkg/sizing"
	"github.com/grafana/loki/pkg/util/flagext"
)

type Config struct {
	BytesPerSecond  flagext.ByteSize
	DaysRetention   int
	MonthlyUnitCost sizing.UnitCostInfo
	Simple          bool

	// Templating
	Template string
}

func (c *Config) RegisterFlags(f *flag.FlagSet) {
	f.Var(&c.BytesPerSecond, "bytes-per-second", "[human readable] How many bytes per second the cluster should receive, i.e. (50MB)")
	f.IntVar(&c.DaysRetention, "days-retention", 30, "Number of days you'd like to retain logs for before deleting. For example, \"--days-retention 30\" means retain logs for 30 days before deleting.")
	f.Float64Var(&c.MonthlyUnitCost.CostPerGBMem, "monthly-cost-per-gb-mem", 2.44, "Monthly dollar cost for a megabyte of RAM")
	f.Float64Var(&c.MonthlyUnitCost.CostPerCPU, "monthly-cost-per-cpu", 18.19, "Monthly dollar cost for a CPU")
	f.Float64Var(&c.MonthlyUnitCost.CostPerGBDisk, "monthly-cost-per-gb-disk", 0.187, "Monthly dollar cost for a GB of persistent disk")
	f.Float64Var(&c.MonthlyUnitCost.CostPerGBObjStorage, "monthly-cost-per-gb-obj-storage", 0.023, "Monthly dollar cost for a GB of object storage")
	f.BoolVar(&c.Simple, "simple", false, "Show a simpler compromise between resource minimums and ceilings.")

	// Templating
	f.StringVar(&c.Template, "template", "", "choses a template style to write cluster information to stdout instead of the text cluster plan")
}

func (c *Config) Validate() error {
	if c.BytesPerSecond <= 0 {
		return errors.New("must specify bytes-per-second")
	}

	//Is there a better way to iterate through all fields in a struct?
	if c.DaysRetention < 0 {
		return errors.New("Cannot specify negative days retention")
	}

	if c.MonthlyUnitCost.CostPerGBMem < 0 {
		return errors.New("Cannot specify negative cost per GB Mem")
	}

	if c.MonthlyUnitCost.CostPerCPU < 0 {
		return errors.New("Cannot specify negative cost per CPU")
	}

	if c.MonthlyUnitCost.CostPerGBDisk < 0 {
		return errors.New("Cannot specify negative cost per GB Disk")
	}

	if c.MonthlyUnitCost.CostPerGBObjStorage < 0 {
		return errors.New("Cannot specify negative cost per GB Object Storage")
	}

	if c.Template != "" && Templaters[c.Template] == nil {
		return fmt.Errorf("unexpected template: %s", c.Template)
	}

	return nil
}

func main() {
	var cfg Config
	cfg.RegisterFlags(flag.CommandLine)
	flag.Parse()
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}

	cluster := sizing.SizeCluster(cfg.BytesPerSecond.Val())

	if cfg.Template != "" {
		out := Templaters[cfg.Template].Template(cluster)
		fmt.Print(out)
		return
	}

	printClusterArchitecture(&cluster, &cfg)
}

func printClusterArchitecture(c *sizing.ClusterResources, cfg *Config) {
	totals := c.Totals()
	ingestRate := cfg.BytesPerSecond

	objectStorageRequired := sizing.ComputeObjectStorage(ingestRate, cfg.DaysRetention)
	MonthlyCosts := sizing.ComputeMonthlyCost(&cfg.MonthlyUnitCost, objectStorageRequired, totals)

	fmt.Printf("Requirements for a Loki cluster than can ingest %v per second with %d days retention\n", sizing.ReadableBytes(ingestRate), cfg.DaysRetention)
	fmt.Printf("\tNodes\n")
	fmt.Printf("\t\tRequired count: %d\n", c.NumNodes())

	fmt.Println("\tMemory")
	if cfg.Simple {
		fmt.Printf("\t\t: %v\n", sizing.ReadableBytes(compromise(totals.MemoryRequests.Val(), totals.MemoryLimits.Val())))
	} else {
		fmt.Printf("\t\tMinimum: %v\n", sizing.ReadableBytes(totals.MemoryRequests))
		fmt.Printf("\t\tWith peak expected usage of: %v\n", sizing.ReadableBytes(totals.MemoryLimits))
	}

	fmt.Println("\tCPU")
	if cfg.Simple {
		fmt.Printf("\t\t: %v\n", sizing.CPUSize(compromise(totals.CPURequests.Millis(), totals.CPULimits.Millis())))
	} else {
		fmt.Printf("\t\tMinimum count: %v CPUs\n", totals.CPURequests)
		fmt.Printf("\t\tWith peak expected usage of: %v CPUs\n", totals.CPULimits)
	}

	fmt.Println("\tStorage")
	fmt.Printf("\t\t%v Disk\n", sizing.ReadableBytes(totals.DiskGB<<30))
	fmt.Printf("\t\t%v Object Storage\n", sizing.ReadableBytes(objectStorageRequired))

	fmt.Printf("\n")

	fmt.Printf("Your expected monthly hardware costs would be approximately\n")
	if cfg.Simple {
		fmt.Printf("\t$%d\n", compromise(int(MonthlyCosts.BaseLoadCost), int(MonthlyCosts.PeakCost)))
	} else {
		fmt.Printf("\tIf you used the minimum required hardware: $%.2f\n", MonthlyCosts.BaseLoadCost)
		fmt.Printf("\tIf you used the peak required hardware: $%.2f\n\n", MonthlyCosts.PeakCost)
	}
	fmt.Printf("This assumes a monthly cost of:\n")
	fmt.Printf("\t$%.2f per CPU\n", cfg.MonthlyUnitCost.CostPerCPU)
	fmt.Printf("\t$%.2f per GB of memory\n", cfg.MonthlyUnitCost.CostPerGBMem)
	fmt.Printf("\t$%.2f per GB of disk\n", cfg.MonthlyUnitCost.CostPerGBDisk)
	fmt.Printf("\t$%.2f per GB of object storage\n", cfg.MonthlyUnitCost.CostPerGBObjStorage)

	fmt.Printf("\n")
	fmt.Printf("List of all components in the Loki cluster, the number of replicas of each, and the resources required per replica\n")

	for _, component := range c.Components() {
		if component != nil {
			fmt.Printf("%v: %d replicas, each of which requires\n", component.Name, component.Replicas)

			fmt.Println("\tMemory")
			if cfg.Simple {
				fmt.Printf("\t\t: %v\n", sizing.ReadableBytes(
					compromise(
						component.Resources.MemoryRequests.Val(),
						component.Resources.MemoryLimits.Val(),
					)))
			} else {
				fmt.Printf("\t\tMinimum: %v\n", component.Resources.MemoryRequests)
				fmt.Printf("\t\tWith peak expected usage of: %v\n", component.Resources.MemoryLimits)
			}

			fmt.Println("\tCPU")
			if cfg.Simple {
				fmt.Printf("\t\t: %v\n", sizing.CPUSize(compromise(
					component.Resources.CPURequests.Millis(),
					component.Resources.CPULimits.Millis(),
				)))
			} else {
				fmt.Printf("\t\tMinimum count: %v CPUs\n", component.Resources.CPURequests)
				fmt.Printf("\t\tWith peak expected usage of: %v CPUs\n", component.Resources.CPULimits)
			}

			if component.Resources.DiskGB != 0 {
				fmt.Println("\tDisk")
				fmt.Printf("\t\t%d GB\n", component.Resources.DiskGB)
			}
		}
	}

}

func compromise(floor, ceil int) int {
	// return floor + 30% of the difference between floor & ceil. Used for simplified estimations
	return floor + (ceil-floor)*3/10
}
