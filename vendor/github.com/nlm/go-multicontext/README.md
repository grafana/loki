# multicontext

One context.Context to rule them all.

multicontext is a go library providing a way to unify multiple `context.Context`
into one. It supports all standard context features: values, cancellation,
deadlines, timeouts.

It is useful in situations where you are not in charge of building the contexts
that you will use, so you cannot derive one from the other.

For more information, see https://pkg.go.dev/github.com/nlm/go-multicontext
