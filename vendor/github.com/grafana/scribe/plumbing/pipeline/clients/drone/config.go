package drone

import (
	"fmt"

	"github.com/grafana/scribe/plumbing"
	"github.com/grafana/scribe/plumbing/pipeline"
)

type DroneLanguage int

const (
	// The languages that are available when generating a Drone config.
	LanguageYAML DroneLanguage = iota
	LanguageStarlark
)

var argVolumeMap = map[pipeline.Argument]string{
	pipeline.ArgumentDockerSocketFS: "/var/run/docker.sock",
}

var argEnvMap = map[pipeline.Argument]string{
	pipeline.ArgumentCommitSHA:  "$DRONE_COMMIT",
	pipeline.ArgumentCommitRef:  "$DRONE_COMMIT_REF",
	pipeline.ArgumentRemoteURL:  "$DRONE_GIT_SSH_URL",
	pipeline.ArgumentWorkingDir: "$DRONE_REPO_NAME",
}

// The configurer for the Drone client returns equivalent environment variables for different arguments.
func (c *Client) Value(arg pipeline.Argument) (string, error) {
	switch arg.Type {
	case pipeline.ArgumentTypeSecret:
		return secretEnv(arg.Key), nil
	case pipeline.ArgumentTypeUnpackagedFS:
		if val, ok := argVolumeMap[arg]; ok {
			return val, nil
		}
		return "", plumbing.ErrorMissingArgument
	}

	if val, ok := argEnvMap[arg]; ok {
		return val, nil
	}

	return "", fmt.Errorf("could not find equivalent of '%s': %w", arg.Key, plumbing.ErrorMissingArgument)
}
