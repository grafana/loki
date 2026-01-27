package columnar

import "github.com/grafana/loki/v3/pkg/memory"

// Null is an [Array] of null values.
type Null struct {
	validity  memory.Bitmap
	nullCount int
}

var _ Array = (*Null)(nil)

// MakeNull creates a new Null array with the given validity bitmap. MakeNull
// panics if validity contains any bit set to true.
func MakeNull(validity memory.Bitmap) *Null {
	arr := &Null{
		validity: validity,
	}
	arr.init()
	return arr
}

//go:noinline
func (arr *Null) init() {
	if arr.validity.SetCount() > 0 {
		panic("found Null array with non-null values in validity bitmap")
	}
	arr.nullCount = arr.validity.Len()
}

// Len returns the number of values in the array.
func (arr *Null) Len() int { return arr.nullCount }

// Nulls returns the number of values in the array.
func (arr *Null) Nulls() int { return arr.nullCount }

// IsNull returns true for all values in the array.
func (arr *Null) IsNull(_ int) bool { return true }

// Kind returns [KindNull].
func (arr *Null) Kind() Kind { return KindNull }

// Validity returns arr's validity bitmap.
func (arr *Null) Validity() memory.Bitmap { return arr.validity }

func (arr *Null) isDatum() {}
func (arr *Null) isArray() {}
