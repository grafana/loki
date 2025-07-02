package logical

import (
	"fmt"
)

// convertToPlan converts a [Value] into a [Plan]. The value becomes the last
// instruction returned by [Return].
func convertToPlan(value Value) (*Plan, error) {
	var builder ssaBuilder
	value, err := builder.process(value)
	if err != nil {
		return nil, fmt.Errorf("error converting plan to SSA: %w", err)
	}

	// Add the final Return instruction based on the last value.
	builder.instructions = append(builder.instructions, &Return{Value: value})

	return &Plan{Instructions: builder.instructions}, nil
}

// ssaBuilder is a helper type for building SSA forms
type ssaBuilder struct {
	instructions []Instruction
	nextID       int
}

func (b *ssaBuilder) getID() int {
	b.nextID++
	return b.nextID
}

// processPlan processes a logical plan and returns the resulting Value.
func (b *ssaBuilder) process(value Value) (Value, error) {
	switch value := value.(type) {
	case *MakeTable:
		return b.processMakeTablePlan(value)
	case *Select:
		return b.processSelectPlan(value)
	case *Limit:
		return b.processLimitPlan(value)
	case *Sort:
		return b.processSortPlan(value)
	case *RangeAggregation:
		return b.processRangeAggregate(value)
	case *VectorAggregation:
		return b.processVectorAggregation(value)

	case *UnaryOp:
		return b.processUnaryOp(value)
	case *BinOp:
		return b.processBinOp(value)
	case *ColumnRef:
		return b.processColumnRef(value)
	case *Literal:
		return b.processLiteral(value)

	default:
		return nil, fmt.Errorf("unsupported value type %T", value)
	}
}

func (b *ssaBuilder) processMakeTablePlan(plan *MakeTable) (Value, error) {
	if _, err := b.process(plan.Selector); err != nil {
		return nil, err
	}

	plan.id = fmt.Sprintf("%%%d", b.getID())
	b.instructions = append(b.instructions, plan)
	return plan, nil
}

func (b *ssaBuilder) processSelectPlan(plan *Select) (Value, error) {
	// Process the child plan first
	if _, err := b.process(plan.Table); err != nil {
		return nil, err
	} else if _, err := b.process(plan.Predicate); err != nil {
		return nil, err
	}

	// Create a node for the select
	plan.id = fmt.Sprintf("%%%d", b.getID())
	b.instructions = append(b.instructions, plan)
	return plan, nil
}

func (b *ssaBuilder) processLimitPlan(plan *Limit) (Value, error) {
	if _, err := b.process(plan.Table); err != nil {
		return nil, err
	}

	plan.id = fmt.Sprintf("%%%d", b.getID())
	b.instructions = append(b.instructions, plan)
	return plan, nil
}

func (b *ssaBuilder) processSortPlan(plan *Sort) (Value, error) {
	if _, err := b.process(plan.Table); err != nil {
		return nil, err
	}

	plan.id = fmt.Sprintf("%%%d", b.getID())
	b.instructions = append(b.instructions, plan)
	return plan, nil
}

func (b *ssaBuilder) processUnaryOp(value *UnaryOp) (Value, error) {
	if _, err := b.process(value.Value); err != nil {
		return nil, err
	}

	// Create a node for the unary operation
	value.id = fmt.Sprintf("%%%d", b.getID())
	b.instructions = append(b.instructions, value)
	return value, nil
}

func (b *ssaBuilder) processRangeAggregate(plan *RangeAggregation) (Value, error) {
	if _, err := b.process(plan.Table); err != nil {
		return nil, err
	}

	plan.id = fmt.Sprintf("%%%d", b.getID())
	b.instructions = append(b.instructions, plan)
	return plan, nil
}

func (b *ssaBuilder) processVectorAggregation(plan *VectorAggregation) (Value, error) {
	if _, err := b.process(plan.Table); err != nil {
		return nil, err
	}

	plan.id = fmt.Sprintf("%%%d", b.getID())
	b.instructions = append(b.instructions, plan)
	return plan, nil
}

func (b *ssaBuilder) processBinOp(expr *BinOp) (Value, error) {
	if _, err := b.process(expr.Left); err != nil {
		return nil, err
	} else if _, err := b.process(expr.Right); err != nil {
		return nil, err
	}

	expr.id = fmt.Sprintf("%%%d", b.getID())
	b.instructions = append(b.instructions, expr)
	return expr, nil
}

func (b *ssaBuilder) processColumnRef(value *ColumnRef) (Value, error) {
	// Nothing to do.
	return value, nil
}

func (b *ssaBuilder) processLiteral(expr *Literal) (Value, error) {
	// Nothing to do.
	return expr, nil
}
