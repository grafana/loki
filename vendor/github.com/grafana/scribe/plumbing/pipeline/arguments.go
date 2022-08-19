package pipeline

type ArgumentType int

const (
	ArgumentTypeString ArgumentType = iota
	ArgumentTypeInt64
	ArgumentTypeFloat64
	ArgumentTypeBool
	ArgumentTypeSecret
	ArgumentTypeFile
	ArgumentTypeFS
	// An ArgumentTypeUnpackagedFS is used for filesystems that are invariably consistent regardless of operating system.
	// Developers can get around packaging and unpackaging of large directories using this argument type.
	// Filesystems and directories used with this argument should always exist on every machine. This basically means that they should be available within the source tree.
	// If this argument type is used for directories outside of the source tree, then expect divergeant behavior between operating systems.
	ArgumentTypeUnpackagedFS
)

var argumentTypeStr = []string{"string", "int", "float", "bool", "secret", "file", "directory", "unpackaged-directory"}

func (a ArgumentType) String() string {
	i := int(a)
	return argumentTypeStr[i]
}

func ArgumentTypesEqual(arg Argument, argTypes ...ArgumentType) bool {
	for _, v := range argTypes {
		if arg.Type == v {
			return true
		}
	}
	return false
}

// An Argument is a pre-defined argument that is used in a typical CI pipeline.
// This allows the scribe library to define different methods of retrieving the same information
// in different run modes.
// For example, when running in CLI or Docker mode, getting the git ref might be as simple as running `git rev-parse HEAD`.
// But in a Drone pipeline, that information may be available before the repository has been cloned in an environment variable.
// Other arguments may require the user to be prompted if they have not been provided.
// These arguments can be provided to the CLI by using the flag `-arg`, for example `-arg=workdir=./example` will set the "workdir" argument to "example" in the CLI run-mode
// By default, all steps expect a WorkingDir and Repository.
type Argument struct {
	Type ArgumentType
	Key  string
}

func NewStringArgument(key string) Argument {
	return Argument{
		Type: ArgumentTypeString,
		Key:  key,
	}
}

func NewInt64Argument(key string) Argument {
	return Argument{
		Type: ArgumentTypeInt64,
		Key:  key,
	}
}

func NewFloat64Argument(key string) Argument {
	return Argument{
		Type: ArgumentTypeFloat64,
		Key:  key,
	}
}

func NewBoolArgument(key string) Argument {
	return Argument{
		Type: ArgumentTypeBool,
		Key:  key,
	}
}

func NewFileArgument(key string) Argument {
	return Argument{
		Type: ArgumentTypeFile,
		Key:  key,
	}
}

func NewDirectoryArgument(key string) Argument {
	return Argument{
		Type: ArgumentTypeFS,
		Key:  key,
	}
}

func NewUnpackagedDirectoryArgument(key string) Argument {
	return Argument{
		Type: ArgumentTypeUnpackagedFS,
		Key:  key,
	}
}

func NewSecretArgument(key string) Argument {
	return Argument{
		Type: ArgumentTypeSecret,
		Key:  key,
	}
}
