package cache

import "golang.org/x/exp/constraints"

// Number is a constraint that permits any numeric types.
type Number interface {
	constraints.Integer | constraints.Float | constraints.Complex
}
