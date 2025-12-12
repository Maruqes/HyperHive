package docker

import (
	"bufio"
	"context"
	"strings"

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
				Links:               es.Links,
				Aliases:             es.Aliases,
				MacAddress:          es.MacAddress,
				DriverOpts:          es.DriverOpts,
				NetworkID:           es.NetworkID,
				EndpointID:          es.EndpointID,
				Gateway:             es.Gateway,
				IPAddress:           es.IPAddress,
				IPPrefixLen:         int32(es.IPPrefixLen),
				IPv6Gateway:         es.IPv6Gateway,
				GlobalIPv6Address:   es.GlobalIPv6Address,
				GlobalIPv6PrefixLen: int32(es.GlobalIPv6PrefixLen),
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
			Id:         conts.ID,
			Names:      conts.Names,
			Image:      conts.Image,
			ImageID:    conts.ImageID,
			Command:    conts.Command,
			Created:    conts.Created,
			Ports:      ports,
			SizeRw:     conts.SizeRw,
			SizeRootFs: conts.SizeRootFs,
			Labels:     conts.Labels,
			State:      state,
			Status:     conts.Status,
			HostConfig: hostConf,
			NetworkSettings: &dockerGRPC.NetworkSettingsSummary{
				Networks: networks,
			},
		})
	}
	return &res, nil
}

func (s *DockerService) ContainerCreateFunc(ctx context.Context, req *dockerGRPC.ContainerCreate) (*dockerGRPC.Empty, error) {

	var ports []PortBinding
	var volumes []VolumeBinding
	var envs []EnvVar

	for _, port := range req.Ports {
		ports = append(ports, PortBinding{ContainerPort: port.ContainerPort, HostPort: port.HostPort})
	}

	for _, vol := range req.Volumes {
		volumes = append(volumes, VolumeBinding{HostPath: vol.HostPath, ContainerPath: vol.ContainerPath})
	}

	for _, env := range req.Envs {
		envs = append(envs, EnvVar{Key: env.Key, Value: env.Value})
	}

	opts := ContainerCreate{
		Image:      req.Image,
		Name:       req.Name,
		Command:    req.Command,
		EntryPoint: req.EntryPoint,

		Ports:   ports,
		Volumes: volumes,
		Envs:    envs,

		Network: req.Network,
		Restart: req.Restart,
		Detach:  req.Detach,

		Memory: req.Memory,
		CPUS:   req.CPUS,
	}

	err := our_container.Create(ctx, &opts)
	return &dockerGRPC.Empty{}, err
}

func (s *DockerService) ContainerRemove(ctx context.Context, req *dockerGRPC.RemoveContainer) (*dockerGRPC.Empty, error) {
	return &dockerGRPC.Empty{}, our_container.Remove(ctx, req.ContainerID, req.Force)
}
func (s *DockerService) ContainerStop(ctx context.Context, req *dockerGRPC.ContainerId) (*dockerGRPC.Empty, error) {
	return &dockerGRPC.Empty{}, our_container.Stop(ctx, req.ContainerID)
}
func (s *DockerService) ContainerStart(ctx context.Context, req *dockerGRPC.ContainerId) (*dockerGRPC.Empty, error) {
	return &dockerGRPC.Empty{}, our_container.Start(ctx, req.ContainerID)
}
func (s *DockerService) ContainerRestart(ctx context.Context, req *dockerGRPC.ContainerId) (*dockerGRPC.Empty, error) {
	return &dockerGRPC.Empty{}, our_container.Restart(ctx, req.ContainerID)
}
func (s *DockerService) ContainerPause(ctx context.Context, req *dockerGRPC.ContainerId) (*dockerGRPC.Empty, error) {
	return &dockerGRPC.Empty{}, our_container.Pause(ctx, req.ContainerID)
}
func (s *DockerService) ContainerUnPause(ctx context.Context, req *dockerGRPC.ContainerId) (*dockerGRPC.Empty, error) {
	return &dockerGRPC.Empty{}, our_container.Unpause(ctx, req.ContainerID)
}
func (s *DockerService) ContainerKill(ctx context.Context, req *dockerGRPC.KillContainer) (*dockerGRPC.Empty, error) {
	return &dockerGRPC.Empty{}, our_container.Kill(ctx, req.ContainerID, req.Signal)
}

func (s *DockerService) ContainerLogs(req *dockerGRPC.ContainerLogsRequest, stream dockerGRPC.DockerService_ContainerLogsServer) error {
	// open logs reader from docker client
	rc, err := our_container.Logs(stream.Context(), req.ContainerID, req.Follow, int(req.Tail), req.Since)
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(rc)

	for scanner.Scan() {
		line := scanner.Text()
		err := stream.Send(&dockerGRPC.LogChunk{Data: []byte(line)})
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *DockerService) ContainerUpdate(ctx context.Context, req *dockerGRPC.ContainerUpdateRequest) (*dockerGRPC.ContainerUpdateResponse, error) {
	res, err := our_container.Update(ctx, req.ContainerID, req.Memory, req.CPUS, req.Restart)
	if err != nil {
		return nil, err
	}
	return &dockerGRPC.ContainerUpdateResponse{Warnings: res.Warnings}, nil
}

func (s *DockerService) ContainerRename(ctx context.Context, req *dockerGRPC.ContainerRenameRequest) (*dockerGRPC.Empty, error) {
	return &dockerGRPC.Empty{}, our_container.Rename(ctx, req.ContainerID, req.NewName)
}

func (s *DockerService) ContainerExec(ctx context.Context, req *dockerGRPC.ExecMsg) (*dockerGRPC.Empty, error) {
	return &dockerGRPC.Empty{}, our_container.Exec(ctx, req.ContainerId, req.Commands)
}

func (s *DockerService) VolumeCreateBindMount(ctx context.Context, req *dockerGRPC.VolumeCreateRequest) (*dockerGRPC.Empty, error) {
	return &dockerGRPC.Empty{}, our_volume.CreateBindMountVolume(ctx, &VolumeCreateRequest{
		Name:   req.Name,
		Folder: req.Folder,
		Labels: req.Labels,
	})
}

func (s *DockerService) VolumeRemove(ctx context.Context, req *dockerGRPC.VolumeRemoveRequest) (*dockerGRPC.Empty, error) {
	return &dockerGRPC.Empty{}, our_volume.Remove(ctx, req.VolumeId, req.Force)
}

func (s *DockerService) VolumeList(ctx context.Context, req *dockerGRPC.Empty) (*dockerGRPC.ListVolumesResponse, error) {
	var res dockerGRPC.ListVolumesResponse

	volumes, err := our_volume.List(ctx)
	if err != nil {
		return nil, err
	}

	for _, v := range volumes.Volumes {
		// Convert Status map[string]interface{} to map[string]string
		status := make(map[string]string)
		for k, val := range v.Status {
			if str, ok := val.(string); ok {
				status[k] = str
			}
		}

		var usageData *dockerGRPC.UsageData
		if v.UsageData != nil {
			usageData = &dockerGRPC.UsageData{
				RefCount: int64(v.UsageData.RefCount),
				Size:     int64(v.UsageData.Size),
			}
		}

		// Get disk space information for the mountpoint
		total, free, used, err := our_volume.GetDiskSpace(v.Mountpoint)
		var diskSpace *dockerGRPC.DiskSpace
		if err == nil {
			diskSpace = &dockerGRPC.DiskSpace{
				Total: int64(total),
				Free:  int64(free),
				Used:  int64(used),
			}
		}

		res.Volumes = append(res.Volumes, &dockerGRPC.Volume{
			CreatedAt:  v.CreatedAt,
			Driver:     v.Driver,
			Labels:     v.Labels,
			Mountpoint: v.Mountpoint,
			Name:       v.Name,
			Options:    v.Options,
			Scope:      v.Scope,
			Status:     status,
			UsageData:  usageData,
			DiskSpace:  diskSpace,
		})
	}

	return &res, nil
}

func (s *DockerService) NetworkCreate(ctx context.Context, req *dockerGRPC.NetworkCreateRequest) (*dockerGRPC.Empty, error) {
	return &dockerGRPC.Empty{}, our_network.Create(ctx, req.Name, NetworkCreateParams{Type: NetworkType(strings.ToLower(req.Params.Type.String())), Subnet: req.Params.Subnet, Gateway: req.Params.Gateway, Parent: req.Params.Parent})
}

func (s *DockerService) NetworkRemove(ctx context.Context, req *dockerGRPC.NetworkRemoveRequest) (*dockerGRPC.Empty, error) {
	return &dockerGRPC.Empty{}, our_network.Remove(ctx, req.Name)
}

func (s *DockerService) NetworkList(ctx context.Context, req *dockerGRPC.Empty) (*dockerGRPC.NetworkListResponse, error) {
	var res dockerGRPC.NetworkListResponse

	networks, err := our_network.List(ctx)
	if err != nil {
		return nil, err
	}

	for _, net := range networks {
		// Map IPAM
		var ipam *dockerGRPC.IPAM
		if net.IPAM.Driver != "" || len(net.IPAM.Config) > 0 {
			var ipamConfigs []*dockerGRPC.IPAMConfig
			for _, cfg := range net.IPAM.Config {
				ipamConfigs = append(ipamConfigs, &dockerGRPC.IPAMConfig{
					Subnet:  cfg.Subnet,
					Gateway: cfg.Gateway,
				})
			}
			ipam = &dockerGRPC.IPAM{
				Driver: net.IPAM.Driver,
				Config: ipamConfigs,
			}
		}

		// Map ConfigFrom
		var configFrom *dockerGRPC.ConfigReference
		if net.ConfigFrom.Network != "" {
			configFrom = &dockerGRPC.ConfigReference{
				Network: net.ConfigFrom.Network,
			}
		}

		// Map Containers (EndpointResource)
		containers := make(map[string]*dockerGRPC.EndpointResource)
		for id, endpoint := range net.Containers {
			containers[id] = &dockerGRPC.EndpointResource{
				Name:        endpoint.Name,
				EndpointID:  endpoint.EndpointID,
				MacAddress:  endpoint.MacAddress,
				IPv4Address: endpoint.IPv4Address,
				IPv6Address: endpoint.IPv6Address,
			}
		}

		// Map Peers
		var peers []*dockerGRPC.PeerInfo
		for _, peer := range net.Peers {
			peers = append(peers, &dockerGRPC.PeerInfo{
				Name: peer.Name,
				IP:   peer.IP,
			})
		}

		// Map Services
		services := make(map[string]*dockerGRPC.ServiceInfo)
		for name, svc := range net.Services {
			var tasks []*dockerGRPC.Task
			for _, task := range svc.Tasks {
				tasks = append(tasks, &dockerGRPC.Task{
					Name:       task.Name,
					EndpointID: task.EndpointID,
					EndpointIP: task.EndpointIP,
					Info:       task.Info,
				})
			}
			services[name] = &dockerGRPC.ServiceInfo{
				VIP:          svc.VIP,
				Ports:        svc.Ports,
				LocalLBIndex: int32(svc.LocalLBIndex),
				Tasks:        tasks,
			}
		}

		res.Networks = append(res.Networks, &dockerGRPC.NetworkSummary{
			Name:       net.Name,
			Id:         net.ID,
			Scope:      net.Scope,
			Driver:     net.Driver,
			IPAM:       ipam,
			Internal:   net.Internal,
			Attachable: net.Attachable,
			Ingress:    net.Ingress,
			ConfigFrom: configFrom,
			ConfigOnly: net.ConfigOnly,
			Containers: containers,
			Options:    net.Options,
			Labels:     net.Labels,
			Peers:      peers,
			Services:   services,
		})
	}

	return &res, nil
}

func (s *DockerService) GitClone(ctx context.Context, req *dockerGRPC.GitCloneReq) (*dockerGRPC.Empty, error) {
	return &dockerGRPC.Empty{}, our_git.GitClone(ctx, req.Url, req.FolderToRun, req.Name, req.Id, req.EnvVars)
}

func (s *DockerService) GitList(ctx context.Context, req *dockerGRPC.Empty) (*dockerGRPC.GitListReq, error) {
	var res dockerGRPC.GitListReq

	list, err := our_git.GitList(ctx)
	if err != nil {
		return nil, err
	}

	for _, item := range list.Elems {
		res.Elems = append(res.Elems, &dockerGRPC.GitListReq_Elem{
			Name:     item.Name,
			RepoLink: item.RepoLink,
		})
	}

	return &res, nil
}

func (s *DockerService) GitRemove(ctx context.Context, req *dockerGRPC.GitRemoveReq) (*dockerGRPC.Empty, error) {
	return &dockerGRPC.Empty{}, our_git.GitRemove(ctx, req.Name, req.FolderToRun, req.Id, req.EnvVars)
}

func (s *DockerService) GitUpdate(ctx context.Context, req *dockerGRPC.GitUpdateReq) (*dockerGRPC.Empty, error) {
	return &dockerGRPC.Empty{}, our_git.GitUpdate(ctx, req.Name, req.FolderToRun, req.Id, req.EnvVars)
}

func (s *DockerService) StartAlwaysContainers(ctx context.Context, req *dockerGRPC.Empty) (*dockerGRPC.Empty, error) {
	return &dockerGRPC.Empty{}, our_git.StartAlwaysContainers(ctx)
}
