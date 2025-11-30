package docker

import (
	"context"

	dockerGRPC "github.com/Maruqes/512SvMan/api/proto/docker"
)

type DockerService struct {
	dockerGRPC.UnimplementedDockerServiceServer
}

var ImagesService *Image

func (s *DockerService) ImageDownload(ctx context.Context, req *dockerGRPC.DownloadImage) (*dockerGRPC.Empty, error) {
	err := ImagesService.Download(ctx, req.ImageRef, req.Registry)
	return &dockerGRPC.Empty{}, err
}

func (s *DockerService) ImageRemove(ctx context.Context, req *dockerGRPC.Remove) (*dockerGRPC.Empty, error) {
	err := ImagesService.Remove(ctx, req.ImageId, req.Force, req.PruneChild)
	return &dockerGRPC.Empty{}, err
}

func (s *DockerService) ImageList(ctx context.Context, req *dockerGRPC.Empty) (*dockerGRPC.ListOfImages, error) {
	imgs, err := ImagesService.List(ctx)
	if err != nil {
		return nil, err
	}
	var res dockerGRPC.ListOfImages

	for _, img := range imgs {
		res.Imgs = append(res.Imgs, &dockerGRPC.ImageSummary{
			Id:          img.ID,
			ParentId:    img.ParentID,
			RepoTags:    img.RepoTags,
			RepoDigests: img.RepoDigests,
			Created:     img.Created,
			Size:        img.Size,
			SharedSize:  img.SharedSize,
			VirtualSize: img.VirtualSize,
			Labels:      img.Labels,
			Containers:  img.Containers,
		})
	}
	return &res, nil
}

func (s *DockerService) ContainerList(ctx context.Context, req *dockerGRPC.Empty) (*dockerGRPC.ListOfContainers, error) {
	containers, err := our_container.List(ctx)
	if err != nil {
		return nil, err
	}
	var res dockerGRPC.ListOfContainers

	for _, conts := range containers {
		// Ports
		var ports []*dockerGRPC.Port
		for _, p := range conts.Ports {
			ports = append(ports, &dockerGRPC.Port{
				IP:          p.IP,
				PrivatePort: uint32(p.PrivatePort),
				PublicPort:  uint32(p.PublicPort),
				Type:        p.Type,
			})
		}

		// HostConfig
		// conts.HostConfig is expected to have NetworkMode and optional Annotations
		hostConf := &dockerGRPC.HostConfig{
			NetworkMode: string(conts.HostConfig.NetworkMode),
			Annotations: conts.HostConfig.Annotations,
		}

		// Network settings
		networks := make(map[string]*dockerGRPC.EndpointSettings)
		for name, es := range conts.NetworkSettings.Networks {
			if es == nil {
				continue
			}
			networks[name] = &dockerGRPC.EndpointSettings{
				Links:                es.Links,
				Aliases:              es.Aliases,
				MacAddress:           es.MacAddress,
				DriverOpts:           es.DriverOpts,
				NetworkID:            es.NetworkID,
				EndpointID:           es.EndpointID,
				Gateway:              es.Gateway,
				IPAddress:            es.IPAddress,
				IPPrefixLen:          int32(es.IPPrefixLen),
				IPv6Gateway:          es.IPv6Gateway,
				GlobalIPv6Address:    es.GlobalIPv6Address,
				GlobalIPv6PrefixLen:  int32(es.GlobalIPv6PrefixLen),
			}
		}

		// Container state mapping
		var state dockerGRPC.ContainerState
		switch conts.State {
		case "created", "Created", "CREATED":
			state = dockerGRPC.ContainerState_CREATED
		case "running", "Running", "RUNNING":
			state = dockerGRPC.ContainerState_RUNNING
		case "paused", "Paused", "PAUSED":
			state = dockerGRPC.ContainerState_PAUSED
		case "restarting", "Restarting", "RESTARTING":
			state = dockerGRPC.ContainerState_RESTARTING
		case "removing", "Removing", "REMOVING":
			state = dockerGRPC.ContainerState_REMOVING
		case "exited", "Exited", "EXITED":
			state = dockerGRPC.ContainerState_EXITED
		case "dead", "Dead", "DEAD":
			state = dockerGRPC.ContainerState_DEAD
		default:
			state = dockerGRPC.ContainerState_CONTAINER_STATE_UNSPECIFIED
		}

		res.Containers = append(res.Containers, &dockerGRPC.ContainerSummary{
			Id:             conts.ID,
			Names:          conts.Names,
			Image:          conts.Image,
			ImageID:        conts.ImageID,
			Command:        conts.Command,
			Created:        conts.Created,
			Ports:          ports,
			SizeRw:         conts.SizeRw,
			SizeRootFs:     conts.SizeRootFs,
			Labels:         conts.Labels,
			State:          state,
			Status:         conts.Status,
			HostConfig:     hostConf,
			NetworkSettings: &dockerGRPC.NetworkSettingsSummary{
				Networks: networks,
			},
		})
	}
	return &res, nil
}
