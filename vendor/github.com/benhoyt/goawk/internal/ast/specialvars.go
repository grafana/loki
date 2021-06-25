// Special variable constants

package ast

const (
	V_ILLEGAL = iota
	V_ARGC
	V_CONVFMT
	V_FILENAME
	V_FNR
	V_FS
	V_NF
	V_NR
	V_OFMT
	V_OFS
	V_ORS
	V_RLENGTH
	V_RS
	V_RSTART
	V_SUBSEP

	V_LAST = V_SUBSEP
)

var specialVars = map[string]int{
	"ARGC":     V_ARGC,
	"CONVFMT":  V_CONVFMT,
	"FILENAME": V_FILENAME,
	"FNR":      V_FNR,
	"FS":       V_FS,
	"NF":       V_NF,
	"NR":       V_NR,
	"OFMT":     V_OFMT,
	"OFS":      V_OFS,
	"ORS":      V_ORS,
	"RLENGTH":  V_RLENGTH,
	"RS":       V_RS,
	"RSTART":   V_RSTART,
	"SUBSEP":   V_SUBSEP,
}

// SpecialVarIndex returns the "index" of the special variable, or 0
// if it's not a special variable.
func SpecialVarIndex(name string) int {
	return specialVars[name]
}
