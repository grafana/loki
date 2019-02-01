package kingpin

import (
	"io/ioutil"

	"github.com/stretchr/testify/assert"

	"sort"
	"strings"
	"testing"
	"time"
)

func newTestApp() *Application {
	return New("test", "").Terminate(nil)
}

func TestCommander(t *testing.T) {
	c := newTestApp()
	ping := c.Command("ping", "Ping an IP address.")
	pingTTL := ping.Flag("ttl", "TTL for ICMP packets").Short('t').Default("5s").Duration()

	selected, err := c.Parse([]string{"ping"})
	assert.NoError(t, err)
	assert.Equal(t, "ping", selected)
	assert.Equal(t, 5*time.Second, *pingTTL)

	selected, err = c.Parse([]string{"ping", "--ttl=10s"})
	assert.NoError(t, err)
	assert.Equal(t, "ping", selected)
	assert.Equal(t, 10*time.Second, *pingTTL)
}

func TestRequiredFlags(t *testing.T) {
	c := newTestApp()
	c.Flag("a", "a").String()
	c.Flag("b", "b").Required().String()

	_, err := c.Parse([]string{"--a=foo"})
	assert.Error(t, err)
	_, err = c.Parse([]string{"--b=foo"})
	assert.NoError(t, err)
}

func TestRepeatableFlags(t *testing.T) {
	c := newTestApp()
	c.Flag("a", "a").String()
	c.Flag("b", "b").Strings()
	_, err := c.Parse([]string{"--a=foo", "--a=bar"})
	assert.Error(t, err)
	_, err = c.Parse([]string{"--b=foo", "--b=bar"})
	assert.NoError(t, err)
}

func TestInvalidDefaultFlagValueErrors(t *testing.T) {
	c := newTestApp()
	c.Flag("foo", "foo").Default("a").Int()
	_, err := c.Parse([]string{})
	assert.Error(t, err)
}

func TestInvalidDefaultArgValueErrors(t *testing.T) {
	c := newTestApp()
	cmd := c.Command("cmd", "cmd")
	cmd.Arg("arg", "arg").Default("one").Int()
	_, err := c.Parse([]string{"cmd"})
	assert.Error(t, err)
}

func TestArgsRequiredAfterNonRequiredErrors(t *testing.T) {
	c := newTestApp()
	cmd := c.Command("cmd", "")
	cmd.Arg("a", "a").String()
	cmd.Arg("b", "b").Required().String()
	_, err := c.Parse([]string{"cmd"})
	assert.Error(t, err)
}

func TestArgsMultipleRequiredThenNonRequired(t *testing.T) {
	c := newTestApp().Writer(ioutil.Discard)
	cmd := c.Command("cmd", "")
	cmd.Arg("a", "a").Required().String()
	cmd.Arg("b", "b").Required().String()
	cmd.Arg("c", "c").String()
	cmd.Arg("d", "d").String()
	_, err := c.Parse([]string{"cmd", "a", "b"})
	assert.NoError(t, err)
	_, err = c.Parse([]string{})
	assert.Error(t, err)
}

func TestDispatchCallbackIsCalled(t *testing.T) {
	dispatched := false
	c := newTestApp()
	c.Command("cmd", "").Action(func(*ParseContext) error {
		dispatched = true
		return nil
	})

	_, err := c.Parse([]string{"cmd"})
	assert.NoError(t, err)
	assert.True(t, dispatched)
}

func TestTopLevelArgWorks(t *testing.T) {
	c := newTestApp()
	s := c.Arg("arg", "help").String()
	_, err := c.Parse([]string{"foo"})
	assert.NoError(t, err)
	assert.Equal(t, "foo", *s)
}

func TestTopLevelArgCantBeUsedWithCommands(t *testing.T) {
	c := newTestApp()
	c.Arg("arg", "help").String()
	c.Command("cmd", "help")
	_, err := c.Parse([]string{})
	assert.Error(t, err)
}

func TestTooManyArgs(t *testing.T) {
	a := newTestApp()
	a.Arg("a", "").String()
	_, err := a.Parse([]string{"a", "b"})
	assert.Error(t, err)
}

func TestTooManyArgsAfterCommand(t *testing.T) {
	a := newTestApp()
	a.Command("a", "")
	assert.NoError(t, a.init())
	_, err := a.Parse([]string{"a", "b"})
	assert.Error(t, err)
}

func TestArgsLooksLikeFlagsWithConsumeRemainder(t *testing.T) {
	a := newTestApp()
	a.Arg("opts", "").Required().Strings()
	_, err := a.Parse([]string{"hello", "-world"})
	assert.Error(t, err)
}

func TestCommandParseDoesNotResetFlagsToDefault(t *testing.T) {
	app := newTestApp()
	flag := app.Flag("flag", "").Default("default").String()
	app.Command("cmd", "")

	_, err := app.Parse([]string{"--flag=123", "cmd"})
	assert.NoError(t, err)
	assert.Equal(t, "123", *flag)
}

func TestCommandParseDoesNotFailRequired(t *testing.T) {
	app := newTestApp()
	flag := app.Flag("flag", "").Required().String()
	app.Command("cmd", "")

	_, err := app.Parse([]string{"cmd", "--flag=123"})
	assert.NoError(t, err)
	assert.Equal(t, "123", *flag)
}

func TestSelectedCommand(t *testing.T) {
	app := newTestApp()
	c0 := app.Command("c0", "")
	c0.Command("c1", "")
	s, err := app.Parse([]string{"c0", "c1"})
	assert.NoError(t, err)
	assert.Equal(t, "c0 c1", s)
}

func TestSubCommandRequired(t *testing.T) {
	app := newTestApp()
	c0 := app.Command("c0", "")
	c0.Command("c1", "")
	_, err := app.Parse([]string{"c0"})
	assert.Error(t, err)
}

func TestInterspersedFalse(t *testing.T) {
	app := newTestApp().Interspersed(false)
	a1 := app.Arg("a1", "").String()
	a2 := app.Arg("a2", "").String()
	f1 := app.Flag("flag", "").String()

	_, err := app.Parse([]string{"a1", "--flag=flag"})
	assert.NoError(t, err)
	assert.Equal(t, "a1", *a1)
	assert.Equal(t, "--flag=flag", *a2)
	assert.Equal(t, "", *f1)
}

func TestInterspersedTrue(t *testing.T) {
	// test once with the default value and once with explicit true
	for i := 0; i < 2; i++ {
		app := newTestApp()
		if i != 0 {
			t.Log("Setting explicit")
			app.Interspersed(true)
		} else {
			t.Log("Using default")
		}
		a1 := app.Arg("a1", "").String()
		a2 := app.Arg("a2", "").String()
		f1 := app.Flag("flag", "").String()

		_, err := app.Parse([]string{"a1", "--flag=flag"})
		assert.NoError(t, err)
		assert.Equal(t, "a1", *a1)
		assert.Equal(t, "", *a2)
		assert.Equal(t, "flag", *f1)
	}
}

func TestDefaultEnvars(t *testing.T) {
	a := New("some-app", "").Terminate(nil).DefaultEnvars()
	f0 := a.Flag("some-flag", "")
	f0.Bool()
	f1 := a.Flag("some-other-flag", "").NoEnvar()
	f1.Bool()
	f2 := a.Flag("a-1-flag", "")
	f2.Bool()
	_, err := a.Parse([]string{})
	assert.NoError(t, err)
	assert.Equal(t, "SOME_APP_SOME_FLAG", f0.envar)
	assert.Equal(t, "", f1.envar)
	assert.Equal(t, "SOME_APP_A_1_FLAG", f2.envar)
}

func TestBashCompletionOptionsWithEmptyApp(t *testing.T) {
	a := newTestApp()
	context, err := a.ParseContext([]string{"--completion-bash"})
	if err != nil {
		t.Errorf("Unexpected error whilst parsing context: [%v]", err)
	}
	args := a.completionOptions(context)
	assert.Equal(t, []string(nil), args)
}

func TestBashCompletionOptions(t *testing.T) {
	a := newTestApp()
	a.Command("one", "")
	a.Flag("flag-0", "").String()
	a.Flag("flag-1", "").HintOptions("opt1", "opt2", "opt3").String()

	two := a.Command("two", "")
	two.Flag("flag-2", "").String()
	two.Flag("flag-3", "").HintOptions("opt4", "opt5", "opt6").String()

	three := a.Command("three", "")
	three.Flag("flag-4", "").String()
	three.Arg("arg-1", "").String()
	three.Arg("arg-2", "").HintOptions("arg-2-opt-1", "arg-2-opt-2").String()
	three.Arg("arg-3", "").String()
	three.Arg("arg-4", "").HintAction(func() []string {
		return []string{"arg-4-opt-1", "arg-4-opt-2"}
	}).String()

	cases := []struct {
		Args            string
		ExpectedOptions []string
	}{
		{
			Args:            "--completion-bash",
			ExpectedOptions: []string{"help", "one", "three", "two"},
		},
		{
			Args:            "--completion-bash --",
			ExpectedOptions: []string{"--flag-0", "--flag-1", "--help"},
		},
		{
			Args:            "--completion-bash --fla",
			ExpectedOptions: []string{"--flag-0", "--flag-1", "--help"},
		},
		{
			// No options available for flag-0, return to cmd completion
			Args:            "--completion-bash --flag-0",
			ExpectedOptions: []string{"help", "one", "three", "two"},
		},
		{
			Args:            "--completion-bash --flag-0 --",
			ExpectedOptions: []string{"--flag-0", "--flag-1", "--help"},
		},
		{
			Args:            "--completion-bash --flag-1",
			ExpectedOptions: []string{"opt1", "opt2", "opt3"},
		},
		{
			Args:            "--completion-bash --flag-1 opt",
			ExpectedOptions: []string{"opt1", "opt2", "opt3"},
		},
		{
			Args:            "--completion-bash --flag-1 opt1",
			ExpectedOptions: []string{"help", "one", "three", "two"},
		},
		{
			Args:            "--completion-bash --flag-1 opt1 --",
			ExpectedOptions: []string{"--flag-0", "--flag-1", "--help"},
		},

		// Try Subcommand
		{
			Args:            "--completion-bash two",
			ExpectedOptions: []string(nil),
		},
		{
			Args:            "--completion-bash two --",
			ExpectedOptions: []string{"--help", "--flag-2", "--flag-3", "--flag-0", "--flag-1"},
		},
		{
			Args:            "--completion-bash two --flag",
			ExpectedOptions: []string{"--help", "--flag-2", "--flag-3", "--flag-0", "--flag-1"},
		},
		{
			Args:            "--completion-bash two --flag-2",
			ExpectedOptions: []string(nil),
		},
		{
			// Top level flags carry downwards
			Args:            "--completion-bash two --flag-1",
			ExpectedOptions: []string{"opt1", "opt2", "opt3"},
		},
		{
			// Top level flags carry downwards
			Args:            "--completion-bash two --flag-1 opt",
			ExpectedOptions: []string{"opt1", "opt2", "opt3"},
		},
		{
			// Top level flags carry downwards
			Args:            "--completion-bash two --flag-1 opt1",
			ExpectedOptions: []string(nil),
		},
		{
			Args:            "--completion-bash two --flag-3",
			ExpectedOptions: []string{"opt4", "opt5", "opt6"},
		},
		{
			Args:            "--completion-bash two --flag-3 opt",
			ExpectedOptions: []string{"opt4", "opt5", "opt6"},
		},
		{
			Args:            "--completion-bash two --flag-3 opt4",
			ExpectedOptions: []string(nil),
		},
		{
			Args:            "--completion-bash two --flag-3 opt4 --",
			ExpectedOptions: []string{"--help", "--flag-2", "--flag-3", "--flag-0", "--flag-1"},
		},

		// Args complete
		{
			// After a command with an arg with no options, nothing should be
			// shown
			Args:            "--completion-bash three ",
			ExpectedOptions: []string(nil),
		},
		{
			// After a command with an arg, explicitly starting a flag should
			// complete flags
			Args:            "--completion-bash three --",
			ExpectedOptions: []string{"--flag-0", "--flag-1", "--flag-4", "--help"},
		},
		{
			// After a command with an arg that does have completions, they
			// should be shown
			Args:            "--completion-bash three arg1 ",
			ExpectedOptions: []string{"arg-2-opt-1", "arg-2-opt-2"},
		},
		{
			// After a command with an arg that does have completions, but a
			// flag is started, flag options should be completed
			Args:            "--completion-bash three arg1 --",
			ExpectedOptions: []string{"--flag-0", "--flag-1", "--flag-4", "--help"},
		},
		{
			// After a command with an arg that has no completions, and isn't first,
			// nothing should be shown
			Args:            "--completion-bash three arg1 arg2 ",
			ExpectedOptions: []string(nil),
		},
		{
			// After a command with a different arg that also has completions,
			// those different options should be shown
			Args:            "--completion-bash three arg1 arg2 arg3 ",
			ExpectedOptions: []string{"arg-4-opt-1", "arg-4-opt-2"},
		},
		{
			// After a command with all args listed, nothing should complete
			Args:            "--completion-bash three arg1 arg2 arg3 arg4",
			ExpectedOptions: []string(nil),
		},
	}

	for _, c := range cases {
		context, _ := a.ParseContext(strings.Split(c.Args, " "))
		args := a.completionOptions(context)

		sort.Strings(args)
		sort.Strings(c.ExpectedOptions)

		assert.Equal(t, c.ExpectedOptions, args, "Expected != Actual: [%v] != [%v]. \nInput was: [%v]", c.ExpectedOptions, args, c.Args)
	}

}
