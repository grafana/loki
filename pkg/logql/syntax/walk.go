package syntax

type WalkFn = func(e Expr)

type Walkable interface {
	Walk(f WalkFn)
}
