// Package jsonsyntax passes the syntax errors of the package jsontext to encoding/json, which reports them in its
// own words: jsontext keeps how an error is made, which encoding/json can't see from outside of it.
package jsonsyntax

// LegacyError is set by the package jsontext. It returns the message and the offset of err, an error of
// jsontext, as encoding/json reports a syntax error, and true; or false if err is not a syntax error.
var LegacyError func(err error) (msg string, offset int64, ok bool)
