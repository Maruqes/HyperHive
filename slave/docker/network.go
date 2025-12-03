package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/network"
)

type Network struct{}

var our_network *Network

type NetworkType string

const (
	NetworkTypeBridge  NetworkType = "bridge"
	NetworkTypeMacvlan NetworkType = "macvlan"
)

type NetworkCreateParams struct {
	// Tipo de network: "bridge" ou "macvlan"
	Type NetworkType `json:"type"`

	// Só usado em macvlan
	Subnet  string `json:"subnet,omitempty"`
	Gateway string `json:"gateway,omitempty"`
	Parent  string `json:"parent,omitempty"` // ex: "eth0"
}

func (s *Network) Create(ctx context.Context, name string, p NetworkCreateParams) error {
	switch p.Type {
	case NetworkTypeBridge:
		// bridge simples, attachable para containers
		_, err := cli.NetworkCreate(ctx, name, network.CreateOptions{
			Driver:     "bridge",
			Attachable: true,
		})
		return err

	case NetworkTypeMacvlan:
		// validação mínima
		if p.Parent == "" {
			return fmt.Errorf("macvlan network requires a parent interface (ex: eth0)")
		}
		if p.Subnet == "" || p.Gateway == "" {
			return fmt.Errorf("macvlan network requires subnet and gateway")
		}

		_, err := cli.NetworkCreate(ctx, name, network.CreateOptions{
			Driver:     "macvlan",
			Attachable: true,
			Options: map[string]string{
				"parent": p.Parent,
			},
			IPAM: &network.IPAM{
				Config: []network.IPAMConfig{
					{
						Subnet:  p.Subnet,
						Gateway: p.Gateway,
					},
				},
			},
		})
		return err

	default:
		// não deixas criar mais nada que não seja bridge/macvlan
		return fmt.Errorf("unsupported network type: %s (only bridge and macvlan are allowed)", p.Type)
	}
}
func (*Network) Remove(ctx context.Context, name string) error {
	// Remove the default network; ignore not-found via direct API error return.
	return cli.NetworkRemove(ctx, name)
}
func (*Network) List(ctx context.Context) ([]network.Summary, error) {
	return cli.NetworkList(ctx, network.ListOptions{})
}
