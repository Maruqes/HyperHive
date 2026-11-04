package ourk8s

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	k8sGrpc "github.com/Maruqes/512SvMan/api/proto/k8s"
	"github.com/Maruqes/512SvMan/logger"
)

const clusterResourcesTimeout = 5 * time.Second

// Only the fields the network graph shows are decoded: annotations, labels and spec.tls
// have no field here, so they can never reach the proto message.
type kubeList struct {
	Items []kubeItem `json:"items"`
}

type kubeItem struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Type      string `json:"type"`
		ClusterIP string `json:"clusterIP"`
		Ports     []struct {
			Name     string `json:"name"`
			Port     int32  `json:"port"`
			NodePort int32  `json:"nodePort"`
			Protocol string `json:"protocol"`
		} `json:"ports"`
		Rules []struct {
			Host string `json:"host"`
			HTTP struct {
				Paths []struct {
					Path    string `json:"path"`
					Backend struct {
						Service struct {
							Name string `json:"name"`
							Port struct {
								Number int32 `json:"number"`
							} `json:"port"`
						} `json:"service"`
					} `json:"backend"`
				} `json:"paths"`
			} `json:"http"`
		} `json:"rules"`
	} `json:"spec"`
	Status struct {
		Addresses []struct {
			Address string `json:"address"`
		} `json:"addresses"`
		Conditions []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"conditions"`
		LoadBalancer struct {
			Ingress []struct {
				IP string `json:"ip"`
			} `json:"ingress"`
		} `json:"loadBalancer"`
	} `json:"status"`
}

// ClusterResources runs only on the k3s server: agents have no kubeconfig.
func ClusterResources(ctx context.Context) *k8sGrpc.ClusterResources {
	if !AreWeMasterSlave() {
		return &k8sGrpc.ClusterResources{}
	}
	ctx, cancel := context.WithTimeout(ctx, clusterResourcesTimeout)
	defer cancel()
	stdout, stderr, err := runKubectl(ctx, "get", "nodes,services,ingresses", "-A", "-o", "json")
	if err != nil {
		logger.Warnf("read cluster resources: %v: %s", err, stderr)
		return &k8sGrpc.ClusterResources{}
	}
	resources, err := parseClusterResources([]byte(stdout))
	if err != nil {
		logger.Warnf("read cluster resources: %v", err)
		return &k8sGrpc.ClusterResources{}
	}
	return resources
}

func parseClusterResources(data []byte) (*k8sGrpc.ClusterResources, error) {
	var list kubeList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("decode kubectl output: %w", err)
	}
	out := &k8sGrpc.ClusterResources{Available: true}
	for _, item := range list.Items {
		switch item.Kind {
		case "Node":
			node := &k8sGrpc.K8SNode{Name: item.Metadata.Name}
			for _, address := range item.Status.Addresses {
				node.Addresses = append(node.Addresses, address.Address)
			}
			for _, condition := range item.Status.Conditions {
				if condition.Type == "Ready" {
					node.Ready = condition.Status == "True"
				}
			}
			out.Nodes = append(out.Nodes, node)
		case "Service":
			service := &k8sGrpc.K8SServiceInfo{Namespace: item.Metadata.Namespace, Name: item.Metadata.Name, Type: item.Spec.Type, ClusterIp: item.Spec.ClusterIP}
			for _, ingress := range item.Status.LoadBalancer.Ingress {
				if ingress.IP != "" {
					service.LoadBalancerIps = append(service.LoadBalancerIps, ingress.IP)
				}
			}
			for _, port := range item.Spec.Ports {
				service.Ports = append(service.Ports, &k8sGrpc.K8SServicePort{Name: port.Name, Port: port.Port, NodePort: port.NodePort, Protocol: port.Protocol})
			}
			out.Services = append(out.Services, service)
		case "Ingress":
			ingress := &k8sGrpc.K8SIngress{Namespace: item.Metadata.Namespace, Name: item.Metadata.Name}
			for _, rule := range item.Spec.Rules {
				for _, path := range rule.HTTP.Paths {
					// shortcut: a backend naming its port by name reads as port 0; upgrade by
					// looking the name up in the service if the card needs it.
					ingress.Rules = append(ingress.Rules, &k8sGrpc.K8SIngressRule{Host: rule.Host, Path: path.Path, Service: path.Backend.Service.Name, Port: path.Backend.Service.Port.Number})
				}
			}
			out.Ingresses = append(out.Ingresses, ingress)
		}
	}
	return out, nil
}
