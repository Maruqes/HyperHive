package npm

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpstreamUpgradeManifest(t *testing.T) {
	manifest := NPMUpgradeManifest()
	if manifest["target"] != image || manifest["target_id"] != UpstreamImageDigest {
		t.Fatal("upgrade target differs from bootstrap")
	}
	templates := manifest["templates"].(map[string]map[string]string)
	if len(templates) != len(upstreamTemplatePaths) {
		t.Fatal("incomplete template set")
	}
	for name, template := range templates {
		original, err := upstreamTemplates.ReadFile("upstream_templates/" + name)
		if err != nil || template["original"] != string(original) || template["patched"] == string(original) {
			t.Fatal("template missing or adapter did not patch it", name, err)
		}
		if !strings.Contains(template["patched"], "hh_") {
			t.Fatal("missing managed target logic", name)
		}
	}

}

func TestProxyVersionSettings(t *testing.T) {
	trust, access := true, 7
	current := Proxy{TrustForwardedProto: &trust, Locations: []Location{{Path: "/private", AccessListID: &access}}}
	desired := Proxy{Locations: []Location{{Path: "/private"}}}
	preserveProxyVersionSettings(&desired, current)
	if desired.TrustForwardedProto != &trust || desired.Locations[0].AccessListID != &access {
		t.Fatal("omitted settings lost")
	}
	noTrust, noAccess := false, 0
	desired.TrustForwardedProto, desired.Locations[0].AccessListID = &noTrust, &noAccess
	preserveProxyVersionSettings(&desired, current)
	if *desired.TrustForwardedProto || *desired.Locations[0].AccessListID != 0 {
		t.Fatal("explicit settings ignored")
	}
	old := upstreamConsumer{Kind: "proxy", Proxy: &Proxy{Locations: []Location{{Path: "/"}}}}
	upgraded := upstreamConsumer{Kind: "proxy", Proxy: &Proxy{TrustForwardedProto: &noTrust, Locations: []Location{{Path: "/", AccessListID: &noAccess}}}}
	if !consumerMatches(old.raw(), upgraded) {
		t.Fatal("new defaults falsely detected as consumer drift")
	}
	upgraded.Proxy.TrustForwardedProto = &trust
	if consumerMatches(old.raw(), upgraded) {
		t.Fatal("actual settings drift was ignored")
	}
}

func TestUpstreamDockerServiceToken(t *testing.T) {
	container := os.Getenv("HYPERHIVE_TEST_NPM_CONTAINER")
	if container == "" {
		t.Skip("requires disposable exact-image fixture")
	}
	runtime := DockerUpstreamRuntime{Container: container}
	ctx := context.Background()
	directory := t.TempDir()
	_, err := runtime.command(ctx, "exec", container, "python3", "-c", "import sqlite3; sqlite3.connect('/data/database.sqlite').backup(sqlite3.connect('/data/hh-token-test.sqlite'))")
	if err != nil {
		t.Fatal(err)
	}
	for source, target := range map[string]string{"keys.json": "keys.json", "hh-token-test.sqlite": "database.sqlite"} {
		if _, err := runtime.command(ctx, "cp", container+":/data/"+source, filepath.Join(directory, target)); err != nil {
			t.Fatal(err)
		}
	}
	token, err := ServiceToken(ctx, directory)
	if err != nil {
		t.Fatal(err)
	}
	out, err := runtime.node(ctx, `(async()=>{const p=JSON.parse(require('node:fs').readFileSync(0,'utf8'));const r=await fetch('http://127.0.0.1:81/api/nginx/proxy-hosts',{headers:{Authorization:'Bearer '+p.token}});console.log(r.status)})().catch(()=>process.exit(1))`, map[string]string{"token": token})
	var status int
	if err != nil || json.Unmarshal(out, &status) != nil || status != 200 {
		t.Fatalf("service token rejected: status %d, transport %v", status, err)
	}
}
