package compactor

// Limits is the per-tenant configuration the compaction coordinator consults.
// *github.com/grafana/loki/v3/pkg/validation.Overrides satisfies it.
type Limits interface {
	// DataObjCompactionEnabled reports whether compaction should run for the
	// given tenant.
	DataObjCompactionEnabled(userID string) bool
}
