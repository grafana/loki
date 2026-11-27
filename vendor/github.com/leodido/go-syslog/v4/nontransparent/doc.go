// Package nontransparent provides a stream parser for syslog messages framed
// with the non-transparent technique described in RFC 6587 §3.4.2. Messages
// are delimited by a trailer byte, either LF (default) or NUL.
package nontransparent
