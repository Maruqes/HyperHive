package services

import (
	"512SvMan/db"
	"512SvMan/docker"
	"512SvMan/protocol"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	dockerGrpc "github.com/Maruqes/512SvMan/api/proto/docker"
	"github.com/Maruqes/512SvMan/logger"
	"github.com/vishvananda/netlink"
)

type DockerService struct{}

const containerLogsTimeout = 10 * time.Minute

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

func (s *DockerService) ContainerLogs(ctx context.Context, machineName, containerID string, tail int32, streamID string) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	req := &dockerGrpc.ContainerLogsRequest{
		ContainerID: containerID,
		Follow:      true,
		Tail:        tail,
	}

	streamCtx, cancel := context.WithTimeout(ctx, containerLogsTimeout)

	go func() {
		defer cancel()
		if err := docker.ContainerLogs(streamCtx, machine.Connection, req, streamID); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			logger.Errorf("docker container logs stream failed for %s on %s: %v", containerID, machineName, err)
		}
	}()

	return nil
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

func (s *DockerService) VolumeCreateBindMount(ctx context.Context, machineName string, req *dockerGrpc.VolumeCreateRequest, nfsID int) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	if nfsID != 0 {
		if req.GetName() == "" {
			return fmt.Errorf("volume name is required when nfs_id is provided")
		}

		nfsShare, err := db.GetNFSShareByID(ctx, nfsID)
		if err != nil {
			return fmt.Errorf("failed to get NFS share by ID %d: %w", nfsID, err)
		}
		if nfsShare == nil {
			return fmt.Errorf("nfs share with ID %d not found", nfsID)
		}

		target := strings.TrimRight(nfsShare.Target, "/")
		if target == "" {
			target = "/"
		}
		folderPath := fmt.Sprintf("%s/docker/%s", target, req.Name)
		if err := os.MkdirAll(folderPath, 0777); err != nil {
			return fmt.Errorf("failed to create folder %s: %w", folderPath, err)
		}
		req.Folder = folderPath
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

type NetworkParams struct {
	Gateway string
	Parent  string
	Subnet  string
}

func getNetParamsFromIface(ifaceName string) (*NetworkParams, error) {
	params := &NetworkParams{Parent: ifaceName}

	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get interface %s: %w", ifaceName, err)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return nil, fmt.Errorf("failed to get interface addresses for %s: %w", ifaceName, err)
	}

	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if ipNet.IP.To4() == nil {
			continue
		}

		networkIP := ipNet.IP.Mask(ipNet.Mask)
		_, subnet, err := net.ParseCIDR(networkIP.String() + "/" + maskToPrefix(ipNet.Mask))
		if err != nil {
			return nil, fmt.Errorf("failed to calculate subnet for %s: %w", ifaceName, err)
		}

		params.Subnet = subnet.String()
		break
	}

	if params.Subnet == "" {
		log.Printf("no IPv4 subnet found for interface %s", ifaceName)
	}

	// 1) tentar gateway pela própria interface
	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		log.Printf("failed to get netlink handle for interface %s: %v", ifaceName, err)
	} else {
		routes, err := netlink.RouteList(link, netlink.FAMILY_V4)
		if err != nil {
			log.Printf("failed to list IPv4 routes for interface %s: %v", ifaceName, err)
		} else {
			for _, r := range routes {
				// default route dessa interface (0.0.0.0/0)
				if r.Dst == nil && r.Gw != nil {
					params.Gateway = r.Gw.String()
					log.Printf("found per-interface gateway %s for %s", params.Gateway, ifaceName)
					break
				}
			}
		}
	}

	// 2) se ainda não temos gateway, tentar o default route global
	if params.Gateway == "" {
		routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
		if err != nil {
			log.Printf("failed to list global IPv4 routes: %v", err)
		} else {
			for _, r := range routes {
				if r.Dst == nil && r.Gw != nil {
					params.Gateway = r.Gw.String()
					log.Printf("using global default gateway %s for interface %s", params.Gateway, ifaceName)
					break
				}
			}
		}
	}

	// 3) fallback: se ainda não há gateway mas temos subnet, assumir .1
	if params.Gateway == "" && params.Subnet != "" {
		_, ipnet, err := net.ParseCIDR(params.Subnet)
		if err != nil {
			log.Printf("failed to parse subnet %s for guessing gateway: %v", params.Subnet, err)
		} else {
			gw := make(net.IP, len(ipnet.IP))
			copy(gw, ipnet.IP)

			if ip4 := gw.To4(); ip4 != nil {
				// assume first usable IP (.1)
				ip4[3]++
				params.Gateway = ip4.String()
				log.Printf("guessed gateway %s for interface %s from subnet %s", params.Gateway, ifaceName, params.Subnet)
			}
		}
	}

	if params.Gateway == "" {
		log.Printf("no IPv4 gateway found or guessed for interface %s", ifaceName)
	}

	return params, nil
}

func maskToPrefix(mask net.IPMask) string {
	ones, _ := mask.Size()
	return fmt.Sprintf("%d", ones)
}

func (s *DockerService) NetworkCreate(machineName, name, networkType string) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	var params dockerGrpc.NetworkCreateParams
	switch networkType {
	case "bridge":
		params.Type = dockerGrpc.NetworkType_BRIDGE
	case "macvlan":
		params.Type = dockerGrpc.NetworkType_MACVLAN

		netParams, err := getNetParamsFromIface("512rede")
		if err != nil {
			return err
		}

		if netParams != nil {
			params.Gateway = netParams.Gateway
			params.Parent = netParams.Parent
			params.Subnet = netParams.Subnet
			log.Printf("macvlan parameters resolved from %s: gateway=%s parent=%s subnet=%s", netParams.Parent, params.Gateway, params.Parent, params.Subnet)
		}
	default:
		return fmt.Errorf("unknown network type %q: supported values are \"bridge\" or \"macvlan\"", networkType)
	}

	req := &dockerGrpc.NetworkCreateRequest{
		Name:   name,
		Params: &params,
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

func (s *DockerService) GitClone(ctx context.Context, machineName string, link, folderToRun, name, id string, envVars map[string]string) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	if err := docker.GitClone(machine.Connection, &dockerGrpc.GitCloneReq{Url: link, FolderToRun: folderToRun, Name: name, Id: id, EnvVars: envVars}); err != nil {
		return err
	}
	if err := db.UpsertDockerRepo(ctx, machineName, name, folderToRun, envVars); err != nil {
		return fmt.Errorf("store git repo reference: %w", err)
	}
	return nil
}

func (s *DockerService) GitList(machineName string) (*dockerGrpc.GitListReq, error) {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return nil, fmt.Errorf("machine %s is not connected", machineName)
	}

	return docker.GitList(machine.Connection)
}

func (s *DockerService) GitRemove(ctx context.Context, machineName string, name string) error {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return fmt.Errorf("machine %s is not connected", machineName)
	}

	repo, err := db.GetDockerRepo(ctx, machineName, name)
	if err != nil {
		return fmt.Errorf("lookup git repo: %w", err)
	}

	req := &dockerGrpc.GitRemoveReq{
		Name:        name,
		FolderToRun: "",
		Id:          "",
		EnvVars:     make(map[string]string),
	}

	if repo != nil {
		req.FolderToRun = repo.FolderToRun
		req.EnvVars = repo.EnvVars
	}

	if err := docker.GitRemove(machine.Connection, req); err != nil {
		return err
	}

	if err := db.DeleteDockerRepo(ctx, machineName, name); err != nil {
		return fmt.Errorf("delete git repo record: %w", err)
	}

	return nil
}

func (s *DockerService) GitUpdate(ctx context.Context, machineName, name, id string, envVars map[string]string) (map[string]string, error) {
	machine := protocol.GetConnectionByMachineName(machineName)
	if machine == nil || machine.Connection == nil {
		return nil, fmt.Errorf("machine %s is not connected", machineName)
	}

	repo, err := db.GetDockerRepo(ctx, machineName, name)
	if err != nil {
		return nil, fmt.Errorf("lookup git repo: %w", err)
	}
	if repo == nil {
		return nil, fmt.Errorf("git repo %s not found for machine %s", name, machineName)
	}

	finalEnv := repo.EnvVars
	if envVars != nil {
		finalEnv = envVars
	}

	if err := docker.GitUpdate(machine.Connection, &dockerGrpc.GitUpdateReq{FolderToRun: repo.FolderToRun, Name: name, Id: id, EnvVars: finalEnv}); err != nil {
		return nil, err
	}

	if err := db.UpsertDockerRepo(ctx, machineName, name, repo.FolderToRun, finalEnv); err != nil {
		return nil, fmt.Errorf("store git repo reference: %w", err)
	}

	return finalEnv, nil
}

func (s *DockerService) StartAlwaysContainers() error {
	conns := protocol.GetAllGRPCConnections()
	if conns == nil {
		return fmt.Errorf("all grpc conns are nil")
	}

	for _, con := range conns {
		if con == nil {
			continue
		}
		err := docker.StartAlwaysContainers(con)
		if err != nil {
			logger.Error(err.Error())
			return err
		}
	}
	return nil
}
