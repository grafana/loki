package logql

// MaxChildrenDisplay defines the maximum number of children that should be
// shown by explain.
const MaxChildrenDisplay = 3

func (e *LiteralStepEvaluator) Explain(parent Node) {
	b := parent.Child("Literal")
	e.nextEv.Explain(b)
}

func (e *LabelReplaceEvaluator) Explain(parent Node) {
	b := parent.Childf("%s LabelReplace", e.expr.Replacement)
	e.nextEvaluator.Explain(b)
}

func (e *VectorAggEvaluator) Explain(parent Node) {
	b := parent.Childf("[%s, %s] VectorAgg", e.expr.Operation, e.expr.Grouping)
	e.nextEvaluator.Explain(b)
}

func (m *MatrixStepEvaluator) Explain(parent Node) {
	parent.Child("MatrixStep")
}

func (e *VectorStepEvaluator) Explain(parent Node) {
	parent.Child("VectorStep")
}

func (e *ConcatStepEvaluator) Explain(parent Node) {
	b := parent.Child("Concat")
	if len(e.evaluators) < MaxChildrenDisplay {
		for _, child := range e.evaluators {
			child.Explain(b)
		}
	} else {
		e.evaluators[0].Explain(b)
		b.Child("...")
		e.evaluators[len(e.evaluators)-1].Explain(b)
	}
}

func (r *RangeVectorEvaluator) Explain(parent Node) {
	parent.Child("RangeVectorAgg")
}

func (e *AbsentRangeVectorEvaluator) Explain(parent Node) {
	parent.Child("Absent RangeVectorAgg")
}

func (e *BinOpStepEvaluator) Explain(parent Node) {
	b := parent.Childf("%s BinOp", e.expr.Op)
	e.lse.Explain(b)
	e.rse.Explain(b)
}

func (i *VectorIterator) Explain(parent Node) {
	parent.Childf("%f vectorIterator", i.val)
}

func (e *QuantileSketchVectorStepEvaluator) Explain(parent Node) {
	b := parent.Child("QuantileSketchVector")
	e.inner.Explain(b)
}

func (e *mergeOverTimeStepEvaluator) Explain(parent Node) {
	parent.Child("MergeFirstOverTime")
}

func (EmptyEvaluator[SampleVector]) Explain(parent Node) {
	parent.Child("Empty")
}
