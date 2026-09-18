// Package sections locates the sections of a data object that belong to one tenant.
package sections

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// A TenantSet holds the sections of one data object that belong to one tenant.
type TenantSet struct {
	// Streams is the tenant's streams section, or nil when the object holds none for it.
	Streams *dataobj.Section

	// Logs holds the tenant's logs sections, keyed by logs-relative section index.
	//
	// The index counts the logs sections of every tenant in the object, not only this
	// tenant's, so a tenant's sections are not numbered from zero. That is the numbering
	// the metastore's section descriptors use, so a descriptor's index looks up directly
	// here.
	Logs map[int]*dataobj.Section
}

// ForTenant returns the sections of all that belong to tenant.
//
// ForTenant fails when the object holds more than one streams section for the tenant: a
// data object carries at most one, so reading the first alone would silently drop the
// streams the other holds.
//
// A tenant with logs sections but no streams section does not fail: the returned
// [TenantSet.Streams] is nil.
func ForTenant(all dataobj.Sections, tenant string) (TenantSet, error) {
	out := TenantSet{Logs: make(map[int]*dataobj.Section)}

	logsIdx := 0
	for _, sec := range all {
		if sec.Tenant != tenant {
			// Another tenant's logs section still advances the index, which is what keeps it
			// logs-relative across the whole object.
			if logs.CheckSection(sec) {
				logsIdx++
			}
			continue
		}

		switch {
		case streams.CheckSection(sec):
			if out.Streams != nil {
				return TenantSet{}, fmt.Errorf("multiple streams sections for tenant %q within one data object", tenant)
			}
			out.Streams = sec

		case logs.CheckSection(sec):
			out.Logs[logsIdx] = sec
			logsIdx++
		}
	}
	return out, nil
}
