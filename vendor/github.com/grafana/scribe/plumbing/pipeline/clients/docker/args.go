package docker

import (
	"fmt"
	"path/filepath"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/grafana/scribe/plumbing/pipeline"
)

const (
	ScribeContainerPath = "/var/scribe"
)

// KnownVolumes are pre-defined filesystem arguments that need to be mounted in the container in order to have value.
// Most filesystem arguments are packaged and placed into the state directory for other steps to use.
// These, however, exist on the host and must be explicitely mounted using a "bind" mount.
var KnownVolumes = map[pipeline.Argument]func(*pipeline.State) (docker.HostMount, error){
	pipeline.ArgumentSourceFS: func(state *pipeline.State) (docker.HostMount, error) {
		source := state.MustGetDirectoryString(pipeline.ArgumentSourceFS)
		sourceAbs, err := filepath.Abs(source)
		if err != nil {
			return docker.HostMount{}, err
		}

		return docker.HostMount{
			Type:   "bind",
			Source: sourceAbs,
			Target: ScribeContainerPath,
		}, nil
	},
	pipeline.ArgumentDockerSocketFS: func(state *pipeline.State) (docker.HostMount, error) {
		return docker.HostMount{
			Type:   "bind",
			Source: "/var/run/docker.sock",
			Target: "/var/run/docker.sock",
		}, nil
	},
}

// applyArguments applies a slice of arguments, typically from the requirements in a Step, onto the options
// used to run the docker container for a step.
// For example, if the step supplied requires the project (by default all of them do), then the argument type
// ArgumentTypeFS is required and is added to the RunOpts volume.
func applyKnownArguments(state *pipeline.State, opts docker.CreateContainerOptions, args []pipeline.Argument) (docker.CreateContainerOptions, error) {
	// For each argument for this step...
	// If it is a predefined filesystem argument, like ArgumentSourceFS, then it may need to be included in the container volumes / mounts.
	// If it is a predefined string argument, like ArgumentBuildID, then it may need to be included as a `-arg` flag.
	for _, arg := range args {
		switch arg.Type {
		case pipeline.ArgumentTypeUnpackagedFS:
			if val, ok := KnownVolumes[arg]; ok {
				mount, err := val(state)
				if err != nil {
					return opts, err
				}

				opts.HostConfig.Mounts = append(opts.HostConfig.Mounts, mount)
				// TODO: this is a hack to add the `-arg` arguments to the docker command.
				// In the future we should restructure the order in which we process this stuff so we don't have to just append to the string
				opts.Config.Cmd = append(opts.Config.Cmd, fmt.Sprintf("%s=%s", arg.Key, mount.Target))
			}
		}
	}

	return opts, nil
}

// func fsArgument(dir string) (docker.HostMount, error) {
// 	// If they've provided a directory and a separate mountpath, then we can safely not set one
// 	if strings.Contains(dir, ":") {
// 		s := strings.Split(dir, ":")
// 		if len(s) != 2 {
// 			return docker.HostMount{}, errors.New("invalid format. filesystem paths should be formatted: '<source>:<target>'")
// 		}
//
// 		return docker.HostMount{
// 			Type:   "bind",
// 			Source: s[0],
// 			Target: s[1],
// 		}, nil
// 	}
//
// 	// Relative paths should be mounted relative to /var/scribe in the container,
// 	// and have an absolute path for mounting (because docker).
// 	wd, err := os.Getwd()
// 	if err != nil {
// 		return docker.HostMount{}, err
// 	}
//
// 	d, err := filepath.Abs(dir)
// 	if err != nil {
// 		return docker.HostMount{}, err
// 	}
//
// 	rel, err := filepath.Rel(wd, d)
// 	if err != nil {
// 		return docker.HostMount{}, err
// 	}
//
// 	return docker.HostMount{
// 		Type:   "bind",
// 		Source: d,
// 		Target: path.Join(ScribeContainerPath, rel),
// 	}, nil
// }

// // GetVolumeValue will attempt to find the appropriate volume to mount based on the argument provided.
// // Some arguments have known or knowable values, like "ArgumentSourceFS".
// func GetVolumeValue(args *plumbing.PipelineArgs, arg pipeline.Argument) (string, error) {
// 	// If an applicable argument is provided, then we should use that, even if it's a known value.
// 	if val, err := args.ArgMap.Get(arg.Key); err == nil {
// 		return val, nil
// 	}
//
// 	// See if we can find a known value for this FS...
// 	if f, ok := KnownVolumes[arg]; ok {
// 		return f(args)
// 	}
//
// 	// TODO: Should we request via stdin?
// 	return "", nil
// }
