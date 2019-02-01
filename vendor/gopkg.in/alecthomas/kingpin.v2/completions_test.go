package kingpin

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveWithBuiltin(t *testing.T) {
	a := completionsMixin{}

	hintAction1 := func() []string {
		return []string{"opt1", "opt2"}
	}
	hintAction2 := func() []string {
		return []string{"opt3", "opt4"}
	}

	a.builtinHintActions = []HintAction{hintAction1, hintAction2}

	args := a.resolveCompletions()
	assert.Equal(t, []string{"opt1", "opt2", "opt3", "opt4"}, args)
}

func TestResolveWithUser(t *testing.T) {
	a := completionsMixin{}
	hintAction1 := func() []string {
		return []string{"opt1", "opt2"}
	}
	hintAction2 := func() []string {
		return []string{"opt3", "opt4"}
	}

	a.hintActions = []HintAction{hintAction1, hintAction2}

	args := a.resolveCompletions()
	assert.Equal(t, []string{"opt1", "opt2", "opt3", "opt4"}, args)
}

func TestResolveWithCombination(t *testing.T) {
	a := completionsMixin{}
	builtin := func() []string {
		return []string{"opt1", "opt2"}
	}
	user := func() []string {
		return []string{"opt3", "opt4"}
	}

	a.builtinHintActions = []HintAction{builtin}
	a.hintActions = []HintAction{user}

	args := a.resolveCompletions()
	// User provided args take preference over builtin (enum-defined) args.
	assert.Equal(t, []string{"opt3", "opt4"}, args)
}

func TestAddHintAction(t *testing.T) {
	a := completionsMixin{}
	hintFunc := func() []string {
		return []string{"opt1", "opt2"}
	}
	a.addHintAction(hintFunc)

	args := a.resolveCompletions()
	assert.Equal(t, []string{"opt1", "opt2"}, args)
}

func TestAddHintActionBuiltin(t *testing.T) {
	a := completionsMixin{}
	hintFunc := func() []string {
		return []string{"opt1", "opt2"}
	}

	a.addHintActionBuiltin(hintFunc)

	args := a.resolveCompletions()
	assert.Equal(t, []string{"opt1", "opt2"}, args)
}
