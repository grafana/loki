package syntax

// WalkFn is the callback function that gets called whenever a node of the AST is visited.
// The return value indicates whether the traversal should continue with the child nodes.
type WalkFn = func(e Expr) bool

// Walkable denotes a node of the AST that can be traversed.
type Walkable interface {
	Walk(f WalkFn)
}
