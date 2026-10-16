package decoder

import "context"

type OptionFlags uint8

const (
	FirstWinOption OptionFlags = 1 << iota
	ContextOption
	PathOption
	// UseNumberOption decodes a number into interface{} as json.Number instead of float64.
	UseNumberOption
	// DisallowUnknownFieldsOption makes an object key which matches no field of the struct an error.
	DisallowUnknownFieldsOption
	// NoCopyStringOption makes the decoded strings refer to the input instead of a copy of their bytes.
	NoCopyStringOption
)

type Option struct {
	Flags   OptionFlags
	Context context.Context
	Path    *Path
}
