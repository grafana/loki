package syntax

// Wiresmith customtype adapter methods for LineFilter; see
// pkg/push/wiresmith.go for the pattern.

// SizeWiresmith implements the wiresmith customtype contract.
func (lf *LineFilter) SizeWiresmith() int { return lf.Size() }

// MarshalWiresmith implements the wiresmith customtype contract.
func (lf *LineFilter) MarshalWiresmith(buf []byte) (int, error) { return lf.MarshalTo(buf) }

// UnmarshalWiresmith implements the wiresmith customtype contract.
func (lf *LineFilter) UnmarshalWiresmith(buf []byte) error { return lf.Unmarshal(buf) }

// EqualWiresmith implements the wiresmith customtype contract.
func (lf *LineFilter) EqualWiresmith(other any) bool {
	o, ok := coerceLineFilter(other)
	if !ok {
		return false
	}
	return lf.Equal(o)
}

// CompareWiresmith implements the wiresmith customtype contract.
func (lf *LineFilter) CompareWiresmith(other any) int {
	o, ok := coerceLineFilter(other)
	if !ok {
		return -1
	}
	if lf.Equal(o) {
		return 0
	}
	if lf.Match != o.Match {
		if lf.Match < o.Match {
			return -1
		}
		return 1
	}
	if lf.Ty != o.Ty {
		if lf.Ty < o.Ty {
			return -1
		}
		return 1
	}
	if lf.Op < o.Op {
		return -1
	}
	return 1
}

func coerceLineFilter(other any) (LineFilter, bool) {
	switch o := other.(type) {
	case LineFilter:
		return o, true
	case *LineFilter:
		if o == nil {
			return LineFilter{}, false
		}
		return *o, true
	default:
		return LineFilter{}, false
	}
}
