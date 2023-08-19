package k8s

import (
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// Builder is a controller-runtime interface used internally. It copies function from
// sigs.k8s.io/controller-runtime/pkg/builder
//
//counterfeiter:generate . Builder
type Builder interface {
	For(object client.Object, opts ...builder.ForOption) Builder
	Owns(object client.Object, opts ...builder.OwnsOption) Builder
	Watches(src source.Source, handler handler.EventHandler, opts ...builder.WatchesOption) Builder
	WithEventFilter(p predicate.Predicate) Builder
	WithOptions(options controller.Options) Builder
	WithLogConstructor(logConstructor func(*reconcile.Request) logr.Logger) Builder
	Named(name string) Builder
	Complete(r reconcile.Reconciler) error
	Build(r reconcile.Reconciler) (controller.Controller, error)
}

type ctrlBuilder struct {
	bld *builder.Builder
}

// NewCtrlBuilder returns a self-referencing controlled builder
// passthrough wrapper implementing the Builder interface above.
func NewCtrlBuilder(b *builder.Builder) Builder {
	return &ctrlBuilder{bld: b}
}

func (b *ctrlBuilder) For(object client.Object, opts ...builder.ForOption) Builder {
	return &ctrlBuilder{bld: b.bld.For(object, opts...)}
}

func (b *ctrlBuilder) Owns(object client.Object, opts ...builder.OwnsOption) Builder {
	return &ctrlBuilder{bld: b.bld.Owns(object, opts...)}
}

func (b *ctrlBuilder) Watches(src source.Source, handler handler.EventHandler, opts ...builder.WatchesOption) Builder {
	return &ctrlBuilder{bld: b.bld.Watches(src, handler, opts...)}
}

func (b *ctrlBuilder) WithEventFilter(p predicate.Predicate) Builder {
	return &ctrlBuilder{bld: b.bld.WithEventFilter(p)}
}

func (b *ctrlBuilder) WithOptions(opts controller.Options) Builder {
	return &ctrlBuilder{bld: b.bld.WithOptions(opts)}
}

func (b *ctrlBuilder) WithLogConstructor(logConstructor func(*reconcile.Request) logr.Logger) Builder {
	return &ctrlBuilder{bld: b.bld.WithLogConstructor(logConstructor)}
}

func (b *ctrlBuilder) Named(name string) Builder {
	return &ctrlBuilder{bld: b.bld.Named(name)}
}

func (b *ctrlBuilder) Complete(r reconcile.Reconciler) error {
	return b.bld.Complete(r)
}

func (b *ctrlBuilder) Build(r reconcile.Reconciler) (controller.Controller, error) {
	return b.bld.Build(r)
}
