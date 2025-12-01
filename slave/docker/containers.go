package docker

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
)

type Container struct{}

var our_container *Container

type PortBinding struct {
	ContainerPort string // "80/tcp"
	HostPort      string // "8080"
}

type VolumeBinding struct {
	HostPath      string
	ContainerPath string
}

type EnvVar struct {
	Key   string
	Value string
}

type ContainerCreate struct {
	Image      string
	Name       string
	Command    []string
	EntryPoint []string

	Ports   []PortBinding
	Volumes []VolumeBinding
	Envs    []EnvVar

	Network string
	Restart string // "always", "no", "unless-stopped"
	Detach  bool

	Memory int64   // bytes
	CPUS   float64 // 0.5, 1, 2...
}

func (*Container) Create(ctx context.Context, conf *ContainerCreate) error {
	// ---- ENV VARS ----
	envs := []string{}
	for _, e := range conf.Envs {
		envs = append(envs, fmt.Sprintf("%s=%s", e.Key, e.Value))
	}

	// ---- PORTS ----
	exposedPorts := nat.PortSet{}
	portBindings := nat.PortMap{}

	for _, p := range conf.Ports {
		containerPort := nat.Port(p.ContainerPort)
		exposedPorts[containerPort] = struct{}{}
		portBindings[containerPort] = []nat.PortBinding{
			{
				HostIP:   "0.0.0.0",
				HostPort: p.HostPort,
			},
		}
	}

	// ---- VOLUMES ----
	mounts := []mount.Mount{}
	for _, v := range conf.Volumes {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: v.HostPath,
			Target: v.ContainerPath,
		})
	}

	// ---- CONFIG ----
	containerCfg := &container.Config{
		Image:        conf.Image,
		Env:          envs,
		Cmd:          conf.Command,
		Entrypoint:   conf.EntryPoint,
		ExposedPorts: exposedPorts,
	}

	hostCfg := &container.HostConfig{
		PortBindings: portBindings,
		RestartPolicy: container.RestartPolicy{
			Name: container.RestartPolicyMode(conf.Restart),
		},
		Resources: container.Resources{
			Memory:   conf.Memory,
			NanoCPUs: int64(conf.CPUS * 1e9),
		},
		Mounts: mounts,
	}

	// ---- NETWORKS ----
	var networkCfg *network.NetworkingConfig

	switch conf.Network {
	case "":
		// Sem rede explícita -> Docker usa "bridge" por default.
		// Não é preciso mexer em NetworkMode nem em NetworkingConfig.

	case "host":
		// Host network mode: partilha stack do host.
		// Em host mode NÃO se passa NetworkingConfig.
		hostCfg.NetworkMode = container.NetworkMode("host")
		networkCfg = nil

	default:
		// Rede custom (ou "bridge" se quiseres forçar explicitamente)
		hostCfg.NetworkMode = container.NetworkMode(conf.Network)
		networkCfg = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				conf.Network: {},
			},
		}
	}

	// ---- CREATE ----
	resp, err := cli.ContainerCreate(ctx, containerCfg, hostCfg, networkCfg, nil, conf.Name)
	if err != nil {
		return err
	}

	// ---- START if -d ----
	if conf.Detach {
		if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
			return err
		}
	}

	return nil
}

func (*Container) Remove(ctx context.Context, containerID string, force bool) error {
	return cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: force})
}

func (*Container) List(ctx context.Context) ([]container.Summary, error) {
	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, err
	}
	return containers, nil
}

func (*Container) Stop(ctx context.Context, containerID string) error {
	return cli.ContainerStop(ctx, containerID, container.StopOptions{})
}

func (*Container) Start(ctx context.Context, containerID string) error {
	return cli.ContainerStart(ctx, containerID, container.StartOptions{})
}

func (*Container) Kill(ctx context.Context, containerID, signal string) error {
	return cli.ContainerKill(ctx, containerID, signal)
}

func (*Container) Restart(ctx context.Context, containerID string) error {
	return cli.ContainerRestart(ctx, containerID, container.StopOptions{})
}

func (*Container) Pause(ctx context.Context, containerID string) error {
	return cli.ContainerPause(ctx, containerID)
}

func (*Container) Unpause(ctx context.Context, containerID string) error {
	return cli.ContainerUnpause(ctx, containerID)
}

func (*Container) Logs(ctx context.Context, containerID string, follow bool, tail int, since string) (io.ReadCloser, error) {
	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Timestamps: false,
	}

	if tail == 0 {
		opts.Since = since
	} else {
		opts.Tail = fmt.Sprintf("%d", tail)
	}

	return cli.ContainerLogs(ctx, containerID, opts)
}

func (*Container) Update(ctx context.Context, containerID string, memory int64, cpus float64, restart string) (container.UpdateResponse, error) {
	update := container.UpdateConfig{
		Resources: container.Resources{
			Memory:   memory,
			NanoCPUs: int64(cpus * 1e9),
		},
		RestartPolicy: container.RestartPolicy{
			Name: container.RestartPolicyMode(restart),
		},
	}
	return cli.ContainerUpdate(ctx, containerID, update)
}

func (*Container) Rename(ctx context.Context, containerID, newName string) error {
	return cli.ContainerRename(ctx, containerID, newName)
}