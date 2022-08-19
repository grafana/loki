package pipeline

type EnvVarType int

const (
	// EnvVarString should be used whenever the value is known and static.
	EnvVarString EnvVarType = iota

	// EnvVarArgument means that the environment variable will be populated by an argument from the state at run-time.
	// Most (all?) CI services will then leave these values out of the configuration and will be injected when the step runs.
	EnvVarArgument
)

type EnvVar struct {
	Type EnvVarType

	// argument will be populated if this EnvVar is created using the NewEnvArgument function.
	argument Argument

	// str will be populated if this EnvVar is created using the NewEnvString function.
	str string
}

type StepEnv map[string]EnvVar

// NewEnvArgument creates a new EnvVar that will be populated based on an Argument found in the state when the step runs.
func NewEnvArgument(arg Argument) EnvVar {
	return EnvVar{
		Type:     EnvVarArgument,
		argument: arg,
	}
}

// NewEnvString creates a new EnvVar that will be populated with a static string value.
func NewEnvString(val string) EnvVar {
	return EnvVar{
		Type: EnvVarArgument,
		str:  val,
	}
}

// String retrieves the static string value set when using the NewEnvString function.
// If the EnvVar's Type property is not "EnvVarString" then it will panic.
func (e EnvVar) String() string {
	if e.Type != EnvVarString {
		panic("envvar is not a string type, but String() was called")
	}

	return e.str
}

// Argument retrieves the argument value set when using the NewEnvArgument function.
// If the EnvVar's Type property is not "EnvVarString" then it will panic.
func (e EnvVar) Argument() Argument {
	if e.Type != EnvVarArgument {
		panic("envvar is not an argument type, but Argument() was called")
	}

	return e.argument
}
