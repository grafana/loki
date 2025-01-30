package syntax

type WalkFn = func(node Walkable) (bool, error)

type Walkable interface {
	Visit(visit WalkFn) error
}

func Walk(visit WalkFn, nodes ...Walkable) error {
	for _, node := range nodes {
		if node == nil {
			continue
		}
		cont, err := visit(node)
		if err != nil {
			return err
		}
		if cont {
			err = node.Visit(visit)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func ConvertToWalkables[T Expr](nodes []T) []Walkable {
	children := make([]Walkable, 0, len(nodes))
	for _, e := range nodes {
		children = append(children, e)
	}
	return children
}
