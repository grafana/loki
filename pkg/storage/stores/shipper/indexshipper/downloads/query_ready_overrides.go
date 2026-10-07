package downloads

import (
	"errors"
	"flag"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/grafana/dskit/flagext"
)

// AllTenants is the TenantDays key that applies to every tenant without its
// own entry.
const AllTenants = "*"

// TenantDays maps a tenant ID, or AllTenants, to a number of days. As a flag it
// is a comma-separated list of tenant=days pairs, e.g. "tenant-a=7,*=1".
type TenantDays map[string]int

// String implements flag.Value.
func (d TenantDays) String() string {
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%d", k, d[k]))
	}
	return strings.Join(pairs, ",")
}

// Set implements flag.Value.
func (d *TenantDays) Set(s string) error {
	m := TenantDays{}
	for pair := range strings.SplitSeq(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		tenant, days, ok := strings.Cut(pair, "=")
		if !ok || tenant == "" {
			return fmt.Errorf("invalid tenant=days pair %q", pair)
		}
		n, err := strconv.Atoi(days)
		if err != nil {
			return fmt.Errorf("invalid number of days in %q: %w", pair, err)
		}
		m[tenant] = n
	}
	*d = m
	return nil
}

// QueryReadyOverrides changes which tenants' indexes this process keeps query
// ready, and for how many days, without changing the per-tenant limits that
// other processes read. It does not change the common index set, which is
// governed by query_ready_num_days.
type QueryReadyOverrides struct {
	NumDays        TenantDays             `yaml:"num_days"`
	TenantsInclude flagext.StringSliceCSV `yaml:"tenants_include"`
	TenantsExclude flagext.StringSliceCSV `yaml:"tenants_exclude"`
}

// RegisterFlagsWithPrefix registers flags.
func (o *QueryReadyOverrides) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.Var(&o.NumDays, prefix+"num-days",
		"Experimental. Number of days of index to keep query ready per tenant in this process, replacing query_ready_index_num_days for that tenant. Comma-separated tenant=days pairs; the tenant '*' applies to every tenant without its own entry. For example: tenant-a=7,*=1.")
	f.Var(&o.TenantsInclude, prefix+"tenants-include",
		"Experimental. Comma-separated list of tenants whose indexes are kept query ready. If empty, all tenants are.")
	f.Var(&o.TenantsExclude, prefix+"tenants-exclude",
		"Experimental. Comma-separated list of tenants whose indexes are never kept query ready.")
}

// Validate validates the config.
func (o *QueryReadyOverrides) Validate() error {
	for tenant, days := range o.NumDays {
		if days < 0 {
			return fmt.Errorf("query ready overrides: negative number of days for tenant %q", tenant)
		}
	}
	for _, t := range o.TenantsInclude {
		if slices.Contains(o.TenantsExclude, t) {
			return errors.New("query ready overrides: tenant " + t + " is both included and excluded")
		}
	}
	return nil
}

// numDays returns the override for tenant, if there is one.
func (o *QueryReadyOverrides) numDays(tenant string) (int, bool) {
	if n, ok := o.NumDays[tenant]; ok {
		return n, true
	}
	n, ok := o.NumDays[AllTenants]
	return n, ok
}

// allowed reports whether tenant may be kept query ready.
func (o *QueryReadyOverrides) allowed(tenant string) bool {
	if len(o.TenantsInclude) > 0 && !slices.Contains(o.TenantsInclude, tenant) {
		return false
	}
	return !slices.Contains(o.TenantsExclude, tenant)
}

// maxNumDays returns the largest override.
func (o *QueryReadyOverrides) maxNumDays() int {
	largest := 0
	for _, n := range o.NumDays {
		largest = max(largest, n)
	}
	return largest
}
