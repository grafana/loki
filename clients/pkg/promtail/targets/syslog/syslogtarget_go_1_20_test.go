//go:build !go1.21

package syslog

// The error message changed in Go 1.21.
const badCertificateErrorMessage = "remote error: tls: bad certificate"
