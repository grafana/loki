package xcap

import "context"

// RunIsolated calls fn with a context that holds a new region in a new capture. The statistics that fn
// records through that context stay out of the region of ctx.
//
// The region of ctx can hold the statistics of concurrent work. A caller that needs the statistics of
// one call alone, such as the requests of one read, runs the call in an isolated region, and reads
// that region when the call returns.
//
// When fn returns, RunIsolated calls merge with the region of ctx, the isolated region, and the error
// of fn. merge decides which statistics reach the region of ctx, and the region of ctx never receives
// the others. A nil merge hands over every statistic of the isolated region, as
// [Region.MergeObservations] does.
//
// A region that fn starts belongs to the new capture, so the capture of ctx does not hold it.
//
// When ctx holds no region, nobody reads the statistics of fn. RunIsolated then calls fn with ctx,
// and does not call merge.
func RunIsolated[T any](ctx context.Context, name string, fn func(ctx context.Context) (T, error), merge func(parent, isolated *Region, err error)) (T, error) {
	parent := RegionFromContext(ctx)
	if parent == nil {
		return fn(ctx)
	}

	isolatedCtx, capture := NewCapture(ctx, nil)
	isolatedCtx, isolated := StartRegion(isolatedCtx, name)

	// Ending the region and the capture stops work that outlives fn from changing the statistics that
	// merge already handed over.
	defer capture.End()
	defer isolated.End()

	result, err := fn(isolatedCtx)
	if merge == nil {
		parent.MergeObservations(isolated)
	} else {
		merge(parent, isolated, err)
	}
	return result, err
}

// Derivation says that a statistic takes its value from another statistic. See [MergeDerived].
type Derivation struct {
	// Target is the statistic that [MergeDerived] records in the parent region.
	Target *StatisticInt64

	// Source is the integer statistic that [MergeDerived] reads from the isolated region.
	Source Statistic
}

// Derive returns the [Derivation] of target from source.
func Derive(target *StatisticInt64, source Statistic) Derivation {
	return Derivation{Target: target, Source: source}
}

// MergeDerived returns a merge function for [RunIsolated]. For each derivation, it records in the parent
// region, as the target statistic, the value that the isolated region recorded for the source
// statistic. It then hands over every statistic of the isolated region, as [Region.MergeObservations]
// does.
//
// It fits a statistic of a lower layer that counts every call, such as the requests of an instrumented
// bucket. The isolated region holds only the calls of one operation, so the parent region receives
// them under a statistic of that operation.
func MergeDerived(derivations ...Derivation) func(parent, isolated *Region, err error) {
	return func(parent, isolated *Region, _ error) {
		for _, d := range derivations {
			parent.Record(d.Target.Observe(isolated.Int64Value(d.Source)))
		}
		parent.MergeObservations(isolated)
	}
}
