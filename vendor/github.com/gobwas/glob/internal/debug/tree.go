package debug

// Tree renders the matcher tree of a compiled *glob.Pattern.
//
// It is set by package glob at init time: the pattern internals are
// unexported, and this keeps them so while letting the in-module tooling
// (cmd/globtest -v) print them. The format is not stable.
var Tree func(pattern any) string
