package k8s

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6 . Client

// Client is a kubernetes client interface used internally. It copies functions from
// sigs.k8s.io/controller-runtime/pkg/client
type Client interface {
	Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error
	Get(ctx context.Context, key client.ObjectKey, obj client.Object) error
}
