package syntax

type WalkFn = func(e interface{})

//type ExplainFn = func(e interface{}) string

func walkAll(f WalkFn, xs ...Walkable) {
	for _, x := range xs {
		x.Walk(f)
	}
}

//func explainAll(f ExplainFn, xs ...Walkable) {
//	for _, x := range xs {
//		x.Explain(f)
//	}
//}

type Walkable interface {
	Walk(f WalkFn)
	//Explain(f ExplainFn)
}
