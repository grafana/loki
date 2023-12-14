package syntax

type WalkFn = func(e Expr)

func walkAll(f WalkFn, xs ...Walkable) {
	for _, x := range xs {
		x.Walk(f)
	}
}

type Walkable interface {
	Walk(f WalkFn)
}

type AcceptVisitor interface {
	Accept(RootVisitor)
}

type RootVisitor interface {
	SampleExprVisitor
	LogSelectorExprVisitor
	StageExprVisitor

	VisitLogRange(*LogRange)
}

type SampleExprVisitor interface {
	VisitBinOp(*BinOpExpr)
	VisitVectorAggregation(*VectorAggregationExpr)
	VisitRangeAggregation(*RangeAggregationExpr)
	VisitLabelReplace(*LabelReplaceExpr)
	VisitLiteral(*LiteralExpr)
	VisitVector(*VectorExpr)
}

type LogSelectorExprVisitor interface {
	VisitMatchers(*MatchersExpr)
	VisitPipeline(*PipelineExpr)
	VisitLiteral(*LiteralExpr)
	VisitVector(*VectorExpr)
}

type StageExprVisitor interface {
	VisitDecolorize(*DecolorizeExpr)
	VisitDropLabels(*DropLabelsExpr)
	VisitJSONExpressionParser(*JSONExpressionParser)
	VisitKeepLabel(*KeepLabelsExpr)
	VisitLabelFilter(*LabelFilterExpr)
	VisitLabelFmt(*LabelFmtExpr)
	VisitLabelParser(*LabelParserExpr)
	VisitLineFilter(*LineFilterExpr)
	VisitLineFmt(*LineFmtExpr)
	VisitLogfmtExpressionParser(*LogfmtExpressionParser)
	VisitLogfmtParser(*LogfmtParserExpr)
}
