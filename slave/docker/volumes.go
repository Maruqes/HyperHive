package docker

import (
	"context"
	"syscall"

	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

type Volume struct{}

var our_volume *Volume

type VolumeCreateRequest struct {
	Name   string
	Folder string // caminho real no host
	Labels map[string]string
}

func (v *Volume) CreateBindMountVolume(ctx context.Context, opts *VolumeCreateRequest) error {
	if cli == nil {
		var err error
		cli, err = client.NewClientWithOpts(client.FromEnv)
		if err != nil {
			return err
		}
		cli.NegotiateAPIVersion(ctx)
	}

	_, err := cli.VolumeCreate(ctx, volume.CreateOptions{
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

// GetDiskSpace returns the total, free, and used space for a given path in bytes
func (v *Volume) GetDiskSpace(path string) (total uint64, free uint64, used uint64, err error) {
	var stat syscall.Statfs_t
	err = syscall.Statfs(path, &stat)
	if err != nil {
		return 0, 0, 0, err
	}

	// Available blocks * size per block = available space in bytes
	total = stat.Blocks * uint64(stat.Bsize)
	free = stat.Bavail * uint64(stat.Bsize)
	used = total - (stat.Bfree * uint64(stat.Bsize))

	return total, free, used, nil
}
