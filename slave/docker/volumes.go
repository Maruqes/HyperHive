package docker

import (
	"context"

	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

type Volume struct{}

type VolumeCreateRequest struct {
	Name   string
	Folder string // caminho real no host
	Labels map[string]string
}

func CreateBindMountVolume(ctx context.Context, opts *VolumeCreateRequest) error {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return err
	}
	cli.NegotiateAPIVersion(ctx)

	_, err = cli.VolumeCreate(ctx, volume.CreateOptions{
		Name:   opts.Name,
		Driver: "local",
		DriverOpts: map[string]string{
			"type":   "none",
			"device": opts.Folder,
			"o":      "bind",
		},
		Labels: opts.Labels,
	})

	return err
}

func (v *Volume) Remove(ctx context.Context, volumeId string, force bool) error {
	return cli.VolumeRemove(ctx, volumeId, force)
}

func (v *Volume) List(ctx context.Context) (volume.ListResponse, error) {
	return cli.VolumeList(ctx, volume.ListOptions{})
}
