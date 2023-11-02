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

type Visitor interface {
	SampleExprVisitor
	LogSelectorExprVisitor
	StageExprVisitor
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
	VisitKeekLabel(*KeepLabelsExpr)
	VisitLabelFilter(*LabelFilterExpr)
	VisitLabelFmt(*LabelFmtExpr)
	VisitLabelParser(*LabelParserExpr)
	VisitLineFilter(*LineFilterExpr)
	VisitLineFmt(*LineFmtExpr)
	VisitLogfmtExpressionParser(*LogfmtExpressionParser)
	VisitLogfmtParser(*LogfmtParserExpr)
}

func Dispatch(expr Expr, v Visitor) {
	switch e := expr.(type) {
	case SampleExpr:
		dispatchSampleExpr(e, v)
	case LogSelectorExpr:
		dispatchLogSelectorExpr(e, v)
	case StageExpr:
		dispatchStageExpr(e, v)
		/*
				case *LogRange
			case *DecolorizeExpr:
				v.VisitDecolorize(e)
			case *DropLabelsExpr:
				v.VisitDropLabels(e)
			case *JSONExpressionParser:
				v.VisitJSONParser(e)
			case *KeepLabelsExpr:
				v.VisitKeepLabels(e)
			case *LabelFilterExpr:
				v.VisitLabelFilter(e)
			case *LabelFmtExpr:
				v.VisitLabelFmt(e)
			case *LabelParserExpr:
				v.VisitLabelParser(e)
			case *LabelReplaceExpr:
				v.VisitLabelReplace(e)
			case *LineFilterExpr:
				v.VisitLineFilter()
			//LineFmtExpr, LiteralExpr, StageExpr
		*/
	}
}

func dispatchSampleExpr(expr SampleExpr, v SampleExprVisitor) {
	switch e := expr.(type) {
	case *BinOpExpr:
		v.VisitBinOp(e)
	case *VectorAggregationExpr:
		v.VisitVectorAggregation(e)
	case *RangeAggregationExpr:
		v.VisitRangeAggregation(e)
	case *LabelReplaceExpr:
		v.VisitLabelReplace(e)
	case *LiteralExpr:
		v.VisitLiteral(e)
	case *VectorExpr:
		v.VisitVector(e)
	}
}

func dispatchLogSelectorExpr(expr LogSelectorExpr, v LogSelectorExprVisitor) {
	switch e := expr.(type) {
	case *PipelineExpr:
		v.VisitPipeline(e)
	case *MatchersExpr:
		v.VisitMatchers(e)
	case *VectorExpr:
		v.VisitVector(e)
	case *LiteralExpr:
		v.VisitLiteral(e)
	}
}

func dispatchStageExpr(expr StageExpr, v StageExprVisitor) {
	switch e := expr.(type) {
	case *DecolorizeExpr:
		v.VisitDecolorize(e)
	case *DropLabelsExpr:
		v.VisitDropLabels(e)
	case *JSONExpressionParser:
		v.VisitJSONExpressionParser(e)
	case *KeepLabelsExpr:
		v.VisitKeekLabel(e)
	case *LabelFilterExpr:
		v.VisitLabelFilter(e)
	case *LabelFmtExpr:
		v.VisitLabelFmt(e)
	case *LabelParserExpr:
		v.VisitLabelParser(e)
	case *LineFilterExpr:
		v.VisitLineFilter(e)
	case *LineFmtExpr:
		v.VisitLineFmt(e)
	case *LogfmtExpressionParser:
		v.VisitLogfmtExpressionParser(e)
	case *LogfmtParserExpr:
		v.VisitLogfmtParser(e)
	}

}
