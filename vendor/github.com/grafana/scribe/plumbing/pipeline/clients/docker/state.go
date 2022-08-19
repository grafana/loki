package docker

import (
	"context"
	"fmt"

	docker "github.com/fsouza/go-dockerclient"
)

func (c *Client) stateVolume(ctx context.Context, id string) (*docker.Volume, error) {
	return c.Client.CreateVolume(docker.CreateVolumeOptions{
		Context: ctx,
		Name:    fmt.Sprintf("scribe-state-%s", id),
	})
}
