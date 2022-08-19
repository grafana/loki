package pipeline

// A StepList is a list of steps that are ran in parallel.
// This type is only used for intermittent storage and should not be used in the Scribe client library
type StepList struct {
	ID           int64
	Steps        []Step
	Dependencies []StepList
	Type         StepType
}

func NewStepList(id int64, steps ...Step) StepList {
	var (
		stepType = StepTypeDefault
	)

	if len(steps) == 0 {
		return StepList{
			ID: id,
		}
	}

	stepType = steps[0].Type

	return StepList{
		ID:    id,
		Steps: steps,
		Type:  stepType,
	}
}
