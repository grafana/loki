package logql

func (e *literalStepEvaluator) Explain(parent Node) {
	b := parent.Child("Literal")
	e.nextEv.Explain(b)
}

func (e *labelReplaceEvaluator) Explain(parent Node) {
	b := parent.Childf("%s LabelReplace", e.expr.Replacement)
	e.nextEvaluator.Explain(b)
}

func (e *vectorAggEvaluator) Explain(parent Node) {
	b := parent.Childf("[%s, %s] VectorAgg", e.expr.Operation, e.expr.Grouping)
	e.nextEvaluator.Explain(b)
}

func (m *MatrixStepEvaluator) Explain(parent Node) {
	parent.Child("MatrixStep")
}

func (e *vectorStepEvaluator) Explain(parent Node) {
	parent.Child("VectorStep")
}

func (e *concatStepEvaluator) Explain(parent Node) {
	b := parent.Child("Concat")
	if len(e.evaluators) < 3 {
		for _, child := range e.evaluators {
			child.Explain(b)
		}
	} else {
		e.evaluators[0].Explain(b)
		b.Child("...")
		e.evaluators[len(e.evaluators)-1].Explain(b)
	}
}

func (r *rangeVectorEvaluator) Explain(parent Node) {
	parent.Child("RangeVectorAgg")
}

func (e *absentRangeVectorEvaluator) Explain(parent Node) {
	parent.Child("Absent RangeVectorAgg")
}

func (e *binOpStepEvaluator) Explain(parent Node) {
	b := parent.Childf("%s BinOp", e.expr.Op)
	e.lse.Explain(b)
	e.rse.Explain(b)
}

func (i *vectorIterator) Explain(parent Node) {
	parent.Childf("%f vectorIterator", i.val)
}
