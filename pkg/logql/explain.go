package logql

import (
	"github.com/xlab/treeprint"
)

func (e *literalStepEvaluator) Explain(parent treeprint.Tree) {
	b := parent.AddBranch("Literal")
	e.nextEv.Explain(b)
}

func (e *labelReplaceEvaluator) Explain(parent treeprint.Tree) {
	b := parent.AddMetaBranch(e.expr.Replacement, "LabelReplace")
	e.nextEvaluator.Explain(b)
}

func (e *vectorAggEvaluator) Explain(parent treeprint.Tree) {
	b := parent.AddMetaBranch([]any{e.expr.Operation, e.expr.Grouping}, "VectorAgg")
	e.nextEvaluator.Explain(b)
}

func (m *MatrixStepEvaluator) Explain(parent treeprint.Tree) {
	parent.AddBranch("MatrixStep")
}

func (e *vectorStepEvaluator) Explain(parent treeprint.Tree) {
	parent.AddBranch("VectorStep")
}

func (e *concatStepEvaluator) Explain(parent treeprint.Tree) {
	b := parent.AddBranch("Concat")
	if len(e.evaluators) < 3 {
		for _, child := range e.evaluators {
			child.Explain(b)
		}
	} else {
		e.evaluators[0].Explain(b)
		b.AddBranch("...")
		e.evaluators[len(e.evaluators)-1].Explain(b)
	}
}

func (r *rangeVectorEvaluator) Explain(parent treeprint.Tree) {
	parent.AddMetaBranch("???", "RangeVectorAgg")
}

func (e *absentRangeVectorEvaluator) Explain(parent treeprint.Tree) {
	parent.AddMetaBranch("Absent", "RangeVectorAgg")
}

func (e *binOpStepEvaluator) Explain(parent treeprint.Tree) {
	b := parent.AddMetaBranch(e.expr.Op, "BinOp")
	e.lse.Explain(b)
	e.rse.Explain(b)
}

func (i *vectorIterator) Explain(parent treeprint.Tree) {
	parent.AddMetaBranch(i.val, "vectorIterator")
}
