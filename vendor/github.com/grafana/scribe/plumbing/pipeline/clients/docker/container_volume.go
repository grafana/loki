package docker

import (
	docker "github.com/fsouza/go-dockerclient"
)

func DefaultMounts(v *docker.Volume) ([]docker.HostMount, error) {
	return []docker.HostMount{
		{
			Type:   "volume",
			Source: v.Name,
			Target: "/opt/scribe",
			TempfsOptions: &docker.TempfsOptions{
				Mode: 777,
			},
		},
	}, nil
}

func MountAt(v *docker.Volume, target string, mode int) docker.HostMount {
	return docker.HostMount{
		Type:   "volume",
		Source: v.Name,
		Target: target,
		TempfsOptions: &docker.TempfsOptions{
			Mode: mode,
		},
	}
}
