package services

import (
	"512SvMan/docker"
	"512SvMan/protocol"
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
