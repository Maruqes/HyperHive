package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Maruqes/512SvMan/logger"
)

const (
	containersTimeout  = 3 * time.Second
	containersMaxBytes = 8 << 20
)

type ContainerNetwork struct {
	Name string
	IP   string
}

type ContainerPort struct {
	IP          string
	PrivatePort uint32
	PublicPort  uint32
	Protocol    string
}

type Container struct {
	Name     string
	Image    string
	State    string
	Networks []ContainerNetwork
	Ports    []ContainerPort
}

// engineContainer decodes only what the map shows, so env and commands are never read.
type engineContainer struct {
	Names  []string
	Image  string
	State  string
	Labels map[string]string
	Ports  []struct {
		IP          string
		PrivatePort uint32
		PublicPort  uint32
		Type        string
	}
	NetworkSettings struct {
		Networks map[string]struct{ IPAddress string }
	}
}

func parseContainers(raw []byte) ([]Container, error) {
	var engine []engineContainer
	if err := json.Unmarshal(raw, &engine); err != nil {
		return nil, fmt.Errorf("decode containers: %w", err)
	}
	out := make([]Container, 0, len(engine))
	for _, c := range engine {
		if len(c.Names) == 0 || isCelldContainer(c.Labels) {
			continue
		}
		container := Container{Name: strings.TrimPrefix(c.Names[0], "/"), Image: c.Image, State: c.State}
		for name, network := range c.NetworkSettings.Networks {
			container.Networks = append(container.Networks, ContainerNetwork{Name: name, IP: network.IPAddress})
		}
		sort.Slice(container.Networks, func(i, j int) bool { return container.Networks[i].Name < container.Networks[j].Name })
		for _, port := range c.Ports {
			container.Ports = append(container.Ports, ContainerPort{IP: port.IP, PrivatePort: port.PrivatePort, PublicPort: port.PublicPort, Protocol: port.Type})
		}
		out = append(out, container)
	}
	return out, nil
}

// isCelldContainer skips fleet workloads: celld members already have their own cards.
func isCelldContainer(labels map[string]string) bool {
	for label := range labels {
		if strings.HasPrefix(label, "celld.") {
			return true
		}
	}
	return false
}

func readEngineContainers(ctx context.Context) ([]byte, error) {
	socket := "/var/run/docker.sock"
	if host := os.Getenv("DOCKER_HOST"); strings.HasPrefix(host, "unix://") {
		socket = strings.TrimPrefix(host, "unix://")
	}
	client := &http.Client{Timeout: containersTimeout, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/json?all=true", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, containersMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK || len(raw) > containersMaxBytes {
		return nil, fmt.Errorf("engine status %d, %d bytes", resp.StatusCode, len(raw))
	}
	return raw, nil
}

// ListContainers reports false rather than an error: a host without Docker is normal.
func ListContainers(ctx context.Context) ([]Container, bool) {
	raw, err := readEngineContainers(ctx)
	if err == nil {
		var containers []Container
		if containers, err = parseContainers(raw); err == nil {
			return containers, true
		}
	}
	logger.Warnf("docker containers unavailable: %v", err)
	return nil, false
}
