package docker

import (
	"context"

	dockerGrpc "github.com/Maruqes/512SvMan/api/proto/docker"
)

type Service struct {
	dockerGrpc.UnimplementedDockerServiceServer
}

func (s *Service) ListContainers(ctx context.Context, _ *dockerGrpc.Empty) (*dockerGrpc.ContainerList, error) {
	containers, available := ListContainers(ctx)
	out := &dockerGrpc.ContainerList{Available: available}
	for _, c := range containers {
		container := &dockerGrpc.Container{Name: c.Name, Image: c.Image, State: c.State}
		for _, network := range c.Networks {
			container.Networks = append(container.Networks, &dockerGrpc.ContainerNetwork{Name: network.Name, Ip: network.IP})
		}
		for _, port := range c.Ports {
			container.Ports = append(container.Ports, &dockerGrpc.ContainerPort{Ip: port.IP, PrivatePort: port.PrivatePort, PublicPort: port.PublicPort, Protocol: port.Protocol})
		}
		out.Containers = append(out.Containers, container)
	}
	return out, nil
}
