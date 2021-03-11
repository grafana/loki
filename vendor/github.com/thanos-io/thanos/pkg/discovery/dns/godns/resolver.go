// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package godns

import (
	"net"

	"github.com/pkg/errors"
)

// Resolver is a wrapper for net.Resolver.
type Resolver struct {
	*net.Resolver
}

// IsNotFound checkout if DNS record is not found.
func (r *Resolver) IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	err = errors.Cause(err)
	dnsErr, ok := err.(*net.DNSError)
	return ok && dnsErr.IsNotFound
}
