// Copyright 2017 Michal Witkowski. All Rights Reserved.
// See LICENSE for licensing terms.

package tags

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors"
)

type reporter struct {
	service, method string

	ctx     context.Context
	opts    *options
	initial bool
}

func (c *reporter) PostCall(error, time.Duration) {}

func (c *reporter) PostMsgSend(interface{}, error, time.Duration) {}

func (c *reporter) PostMsgReceive(m interface{}, _ error, _ time.Duration) {
	if c.initial {
		c.initial = false
		if valMap := c.opts.requestFieldsFunc(interceptors.FullMethod(c.service, c.method), m); valMap != nil {
			t := Extract(c.ctx)
			for k, v := range valMap {
				// This assumes we can modify in place (it depends on tags implementation).
				t.Set("grpc.request."+k, v)
			}
		}
	}
}

type reportable struct {
	opts *options
}

func (r *reportable) ServerReporter(ctx context.Context, _ interface{}, typ interceptors.GRPCType, service string, method string) (interceptors.Reporter, context.Context) {
	tags := extractOrCreate(ctx)
	if peer, ok := peer.FromContext(ctx); ok {
		tags.Set("peer.address", peer.Addr.String())
	}
	newCtx := SetInContext(ctx, tags)
	if r.opts.requestFieldsFunc != nil {
		return &reporter{ctx: newCtx, service: service, method: method, opts: r.opts, initial: true}, newCtx
	}

	return interceptors.NoopReporter{}, newCtx
}

// TODO(bwplotka): Add client, Add request ID / trace ID generation.

// UnaryServerInterceptor returns a new unary server interceptors that sets the values for request tags.
func UnaryServerInterceptor(opts ...Option) grpc.UnaryServerInterceptor {
	o := evaluateOptions(opts)
	return interceptors.UnaryServerInterceptor(&reportable{opts: o})
}

// StreamServerInterceptor returns a new streaming server interceptor that sets the values for request tags.
func StreamServerInterceptor(opts ...Option) grpc.StreamServerInterceptor {
	o := evaluateOptions(opts)
	return interceptors.StreamServerInterceptor(&reportable{opts: o})
}
