package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Maruqes/512SvMan/logger"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
)

type Container struct{}

var our_container *Container

var (
	ErrProtectedContainer = errors.New("operation not allowed on protected container")
	protectedContainers   = []string{"npm", "hyperhive-frontend"}
)

// isProtectedContainer checks if a container is protected by ID or name
func isProtectedContainer(ctx context.Context, containerID string) (bool, error) {
	info, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return false, fmt.Errorf("failed to inspect container: %w", err)
	}

	// Remove leading slash from container name
	containerName := strings.TrimPrefix(info.Name, "/")

	for _, protected := range protectedContainers {
		if strings.Contains(strings.ToLower(containerName), strings.ToLower(protected)) {
			return true, nil
		}
	}
	return false, nil
}

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
	if protected, err := isProtectedContainer(ctx, containerID); err != nil {
		return err
	} else if protected {
		return ErrProtectedContainer
	}
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
	if protected, err := isProtectedContainer(ctx, containerID); err != nil {
		return err
	} else if protected {
		return ErrProtectedContainer
	}
	return cli.ContainerStop(ctx, containerID, container.StopOptions{})
}

func (*Container) Start(ctx context.Context, containerID string) error {
	if protected, err := isProtectedContainer(ctx, containerID); err != nil {
		return err
	} else if protected {
		return ErrProtectedContainer
	}
	return cli.ContainerStart(ctx, containerID, container.StartOptions{})
}

func (*Container) Kill(ctx context.Context, containerID, signal string) error {
	if protected, err := isProtectedContainer(ctx, containerID); err != nil {
		return err
	} else if protected {
		return ErrProtectedContainer
	}
	return cli.ContainerKill(ctx, containerID, signal)
}

func (*Container) Restart(ctx context.Context, containerID string) error {
	if protected, err := isProtectedContainer(ctx, containerID); err != nil {
		return err
	} else if protected {
		return ErrProtectedContainer
	}
	return cli.ContainerRestart(ctx, containerID, container.StopOptions{})
}

func (*Container) Pause(ctx context.Context, containerID string) error {
	if protected, err := isProtectedContainer(ctx, containerID); err != nil {
		return err
	} else if protected {
		return ErrProtectedContainer
	}
	return cli.ContainerPause(ctx, containerID)
}

func (*Container) Unpause(ctx context.Context, containerID string) error {
	if protected, err := isProtectedContainer(ctx, containerID); err != nil {
		return err
	} else if protected {
		return ErrProtectedContainer
	}
	return cli.ContainerUnpause(ctx, containerID)
}

func (*Container) Logs(ctx context.Context, containerID string, follow bool, tail int, since string) (io.ReadCloser, error) {
	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Timestamps: false,
	}

	if tail < 0 {
		opts.Tail = "all"
	} else if tail > 0 {
		opts.Tail = fmt.Sprintf("%d", tail)
	} else if since != "" {
		opts.Since = since
	}

	return cli.ContainerLogs(ctx, containerID, opts)
}

func (*Container) Update(ctx context.Context, containerID string, memory int64, cpus float64, restart string) (container.UpdateResponse, error) {
	if protected, err := isProtectedContainer(ctx, containerID); err != nil {
		return container.UpdateResponse{}, err
	} else if protected {
		return container.UpdateResponse{}, ErrProtectedContainer
	}
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
	if protected, err := isProtectedContainer(ctx, containerID); err != nil {
		return err
	} else if protected {
		return ErrProtectedContainer
	}
	return cli.ContainerRename(ctx, containerID, newName)
}

func (*Container) Exec(ctx context.Context, containerID string, command []string) error {
	if protected, err := isProtectedContainer(ctx, containerID); err != nil {
		return err
	} else if protected {
		return ErrProtectedContainer
	}
	// Transforma o comando numa string
	joined := strings.Join(command, " ")

	// Envia a saída para o PID 1 → vai para docker logs
	redirectCmd := []string{
		"sh", "-c",
		fmt.Sprintf("%s 1>/proc/1/fd/1 2>/proc/1/fd/2", joined),
	}

	execCfg := container.ExecOptions{
		Cmd:          redirectCmd,
		AttachStdout: false,
		AttachStderr: false,
		Tty:          false,
	}

	execID, err := cli.ContainerExecCreate(ctx, containerID, execCfg)
	if err != nil {
		return fmt.Errorf("exec create failed: %w", err)
	}

	resp, err := cli.ContainerExecAttach(ctx, execID.ID, container.ExecAttachOptions{
		Tty: false,
	})
	if err != nil {
		return fmt.Errorf("exec attach failed: %w", err)
	}
	defer resp.Close()

	// Não precisas de ler nada, só deixar correr
	io.Copy(io.Discard, resp.Reader)

	return nil
}

func (g *Git) StartAlwaysContainers(ctx context.Context) error {
	if cli == nil {
		return fmt.Errorf("docker client nil")
	}

	cts, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("docker container list: %w", err)
	}

	for _, c := range cts {
		ins, err := cli.ContainerInspect(ctx, c.ID)
		if err != nil {
			logger.Error("docker inspect failed", "id", shortID(c.ID), "err", err)
			continue
		}

		if ins.HostConfig == nil || ins.HostConfig.RestartPolicy.IsAlways() {
			continue
		}

		if ins.State != nil && ins.State.Running {
			continue
		}

		name := strings.TrimPrefix(ins.Name, "/")
		if name == "" {
			name = shortID(ins.ID)
		}

		var lastErr error
		for attempt := 1; attempt <= 3; attempt++ {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			if err := cli.ContainerStart(ctx, ins.ID, container.StartOptions{}); err == nil {
				lastErr = nil
				break
			} else {
				lastErr = err
				if attempt < 3 {
					logger.Error("docker start failed (retry)", "name", name, "id", shortID(ins.ID), "attempt", attempt, "err", err)
					time.Sleep(2 * time.Second)
				}
			}
		}

		if lastErr != nil {
			logger.Error("docker start failed (giving up)", "name", name, "id", shortID(ins.ID), "attempts", 3, "err", lastErr)
		}
	}

	return nil
}

func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}
