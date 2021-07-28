package logql

type WalkFn = func(e interface{})

func walkAll(f WalkFn, xs ...Walkable) {
	for _, x := range xs {
		x.Walk(f)
	}
}

type Walkable interface {
	Walk(f WalkFn)
}
