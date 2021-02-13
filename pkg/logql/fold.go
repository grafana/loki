package logql

// type signature isn't great. Will be overloaded with receivers
type FoldFn = func(accum interface{}, x interface{}) (interface{}, error)

func foldAll(fn FoldFn, initial interface{}, xs ...Foldable) (interface{}, error) {
	next := initial
	var err error
	for _, x := range xs {
		next, err = x.Fold(fn, next)
		if err != nil {
			return nil, err
		}
	}
	return next, nil
}

// Foldable
// Generally we try to fold child nodes before parents
type Foldable interface {
	Fold(
		FoldFn,
		interface{},
	) (interface{}, error)
}
