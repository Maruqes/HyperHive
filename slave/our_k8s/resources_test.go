package ourk8s

import (
	"context"
	"strings"
	"testing"

	"slave/env512"
)

const clusterFixture = `{"apiVersion":"v1","kind":"List","items":[
{"kind":"Node","metadata":{"name":"alpha","annotations":{"k3s.io/node-args":"annotation-secret"},"labels":{"role":"label-secret"}},
 "status":{"addresses":[{"type":"InternalIP","address":"10.0.0.1"},{"type":"Hostname","address":"alpha"}],"conditions":[{"type":"MemoryPressure","status":"True"},{"type":"Ready","status":"True"}]}},
{"kind":"Service","metadata":{"namespace":"kube-system","name":"traefik","annotations":{"a":"annotation-secret"}},
 "spec":{"type":"LoadBalancer","clusterIP":"10.43.0.5","ports":[{"name":"websecure","port":443,"nodePort":30443,"protocol":"TCP"}]},
 "status":{"loadBalancer":{"ingress":[{"ip":"10.0.0.1"}]}}},
{"kind":"Ingress","metadata":{"namespace":"apps","name":"web","annotations":{"a":"annotation-secret"}},
 "spec":{"tls":[{"hosts":["app.example.com"],"secretName":"tls-secret-name"}],
  "rules":[{"host":"app.example.com","http":{"paths":[{"path":"/","backend":{"service":{"name":"web","port":{"number":8080}}}}]}}]}},
{"kind":"Secret","metadata":{"name":"ignored"}}
]}`

func TestParseClusterResourcesKeepsOnlyListedFields(t *testing.T) {
	got, err := parseClusterResources([]byte(clusterFixture))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Available || len(got.Nodes) != 1 || len(got.Services) != 1 || len(got.Ingresses) != 1 {
		t.Fatalf("unexpected resources: %v", got)
	}
	if node := got.Nodes[0]; !node.Ready || len(node.Addresses) != 2 || node.Addresses[0] != "10.0.0.1" {
		t.Fatalf("node: %v", node)
	}
	service := got.Services[0]
	if service.Type != "LoadBalancer" || service.ClusterIp != "10.43.0.5" || service.Ports[0].NodePort != 30443 || service.LoadBalancerIps[0] != "10.0.0.1" {
		t.Fatalf("service: %v", service)
	}
	rule := got.Ingresses[0].Rules[0]
	if rule.Host != "app.example.com" || rule.Path != "/" || rule.Service != "web" || rule.Port != 8080 {
		t.Fatalf("rule: %v", rule)
	}
	for _, secret := range []string{"annotation-secret", "label-secret", "tls-secret-name"} {
		if strings.Contains(got.String(), secret) {
			t.Fatalf("%s reached the message: %v", secret, got)
		}
	}
}

func TestParseClusterResourcesRejectsInvalidOutput(t *testing.T) {
	if _, err := parseClusterResources([]byte("not json")); err == nil {
		t.Fatal("expected an error")
	}
}

func TestClusterResourcesUnavailableOffServer(t *testing.T) {
	slaveIP, masterIP := env512.SlaveIP, env512.MasterIP
	t.Cleanup(func() { env512.SlaveIP, env512.MasterIP = slaveIP, masterIP })
	env512.SlaveIP, env512.MasterIP = "10.0.0.2", "10.0.0.1"
	if ClusterResources(context.Background()).Available {
		t.Fatal("an agent must report available=false")
	}
}
