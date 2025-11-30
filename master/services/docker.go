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
