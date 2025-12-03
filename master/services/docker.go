package services

import (
	"512SvMan/docker"
	"512SvMan/protocol"
	"context"
	"fmt"

	dockerGrpc "github.com/Maruqes/512SvMan/api/proto/docker"
)

type DockerService struct{}

func (s *DockerService) ImageList(machineName string) (*dockerGrpc.ListOfImages, error) {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return nil, fmt.Errorf("machine %s is not connected", machineName)
	}

	return docker.ImageList(machine.Connection)
}

func (s *DockerService) ImageDownload(machineName, image, registry string) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	req := &dockerGrpc.DownloadImage{
		ImageRef: image,
		Registry: registry,
	}

	return docker.ImageDownload(machine.Connection, req)
}

func (s *DockerService) ImageRemove(machineName, imageID string, force, pruneChild bool) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	req := &dockerGrpc.Remove{
		ImageId:    imageID,
		Force:      force,
		PruneChild: pruneChild,
	}

	return docker.ImageRemove(machine.Connection, req)
}

func (s *DockerService) ContainerList(machineName string) (*dockerGrpc.ListOfContainers, error) {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return nil, fmt.Errorf("machine %s is not connected", machineName)
	}

	return docker.ContainerList(machine.Connection)
}

func (s *DockerService) ContainerCreateFunc(machineName string, req *dockerGrpc.ContainerCreate) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	return docker.ContainerCreateFunc(machine.Connection, req)
}

func (s *DockerService) ContainerRemove(machineName, containerID string, force bool) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	req := &dockerGrpc.RemoveContainer{
		ContainerID: containerID,
		Force:       force,
	}

	return docker.ContainerRemove(machine.Connection, req)
}

func (s *DockerService) ContainerStop(machineName, containerID string) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	req := &dockerGrpc.ContainerId{ContainerID: containerID}
	return docker.ContainerStop(machine.Connection, req)
}

func (s *DockerService) ContainerStart(machineName, containerID string) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	req := &dockerGrpc.ContainerId{ContainerID: containerID}
	return docker.ContainerStart(machine.Connection, req)
}

func (s *DockerService) ContainerRestart(machineName, containerID string) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	req := &dockerGrpc.ContainerId{ContainerID: containerID}
	return docker.ContainerRestart(machine.Connection, req)
}

func (s *DockerService) ContainerPause(machineName, containerID string) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	req := &dockerGrpc.ContainerId{ContainerID: containerID}
	return docker.ContainerPause(machine.Connection, req)
}

func (s *DockerService) ContainerUnpause(machineName, containerID string) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	req := &dockerGrpc.ContainerId{ContainerID: containerID}
	return docker.ContainerUnPause(machine.Connection, req)
}

func (s *DockerService) ContainerKill(machineName, containerID, signal string) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	req := &dockerGrpc.KillContainer{
		ContainerID: containerID,
		Signal:      signal,
	}

	return docker.ContainerKill(machine.Connection, req)
}

func (s *DockerService) ContainerLogs(ctx context.Context, machineName, containerID string, tail int32) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	req := &dockerGrpc.ContainerLogsRequest{
		ContainerID: containerID,
		Follow:      true,
		Tail:        tail,
	}

	return docker.ContainerLogs(ctx, machine.Connection, req)
}

func (s *DockerService) ContainerUpdate(machineName, containerID string, memory int64, cpus float64, restart string) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	req := &dockerGrpc.ContainerUpdateRequest{
		ContainerID: containerID,
		Memory:      memory,
		CPUS:        cpus,
		Restart:     restart,
	}

	return docker.ContainerUpdate(machine.Connection, req)
}

func (s *DockerService) ContainerRename(machineName, containerID, newName string) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	req := &dockerGrpc.ContainerRenameRequest{
		ContainerID: containerID,
		NewName:     newName,
	}

	return docker.ContainerRename(machine.Connection, req)
}

func (s *DockerService) ContainerExec(machineName, containerID string, commands []string) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	req := &dockerGrpc.ExecMsg{
		ContainerId: containerID,
		Commands:    commands,
	}

	return docker.ContainerExec(machine.Connection, req)
}

func (s *DockerService) VolumeList(machineName string) (*dockerGrpc.ListVolumesResponse, error) {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return nil, fmt.Errorf("machine %s is not connected", machineName)
	}

	return docker.VolumeList(machine.Connection)
}

func (s *DockerService) VolumeCreateBindMount(machineName string, req *dockerGrpc.VolumeCreateRequest) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	return docker.VolumeCreateBindMount(machine.Connection, req)
}

func (s *DockerService) VolumeRemove(machineName string, req *dockerGrpc.VolumeRemoveRequest) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	return docker.VolumeRemove(machine.Connection, req)
}

func (s *DockerService) NetworkList(machineName string) (*dockerGrpc.NetworkListResponse, error) {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return nil, fmt.Errorf("machine %s is not connected", machineName)
	}

	return docker.NetworkList(machine.Connection)
}

func (s *DockerService) NetworkCreate(machineName, name, networkType string) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	// TODO: Build the full NetworkCreateRequest with params based on networkType
	req := &dockerGrpc.NetworkCreateRequest{
		Name: name,
		// Params will be populated here
	}

	return docker.NetworkCreate(machine.Connection, req)
}

func (s *DockerService) NetworkRemove(machineName string, req *dockerGrpc.NetworkRemoveRequest) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	return docker.NetworkRemove(machine.Connection, req)
}
