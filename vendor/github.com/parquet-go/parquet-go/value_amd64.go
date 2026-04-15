//go:build !purego

package parquet

import "golang.org/x/sys/cpu"

//go:noescape
func memsetValuesAVX2(values []Value, model Value, _ uint64)

func memsetValues(values []Value, model Value) {
	if cpu.X86.HasAVX2 {
		memsetValuesAVX2(values, model, 0)
	} else {
		for i := range values {
			values[i] = model
		}
	}
}
