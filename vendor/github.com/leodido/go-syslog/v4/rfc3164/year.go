package rfc3164

import (
	"time"
)

// YearOperator is an interface that the operation inferring the year have to implement.
type YearOperator interface {
	Apply() int
}

// YearOperation represents the operation to perform to obtain the year depending on the inner operator/strategy.
type YearOperation struct {
	Operator YearOperator
}

// Operate gets the year depending on the current strategy.
func (y YearOperation) Operate() int {
	return y.Operator.Apply()
}

// CurrentYear is a strategy to obtain the current year in RFC 3164 syslog messages.
type CurrentYear struct{}

// Apply gets the current year
func (CurrentYear) Apply() int {
	return time.Now().Year()
}

// Year is a strategy to obtain the specified year in the RFC 3164 syslog messages.
type Year struct {
	YYYY int
}

// Apply gets the specified year
func (y Year) Apply() int {
	return y.YYYY
}
