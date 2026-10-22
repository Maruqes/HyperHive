package npm

import (
	"512SvMan/db"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// This bridge keeps NPM off host ports while exercising the real HTTP helpers.
func disposableNPMGateway(t *testing.T, runtime DockerUpstreamRuntime, dropNext *atomic.Bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read", 500)
			return
		}
		out, err := runtime.node(r.Context(), `(async()=>{const fs=require('node:fs');const p=JSON.parse(fs.readFileSync(0,'utf8'));const r=await fetch('http://127.0.0.1:81'+p.path,{method:p.method,headers:{Authorization:p.authorization,'Content-Type':'application/json'},body:p.body||undefined});console.log(JSON.stringify({status:r.status,body:await r.text()}))})().catch(()=>process.exit(1));`, map[string]string{"path": r.URL.RequestURI(), "method": r.Method, "authorization": r.Header.Get("Authorization"), "body": string(body)})
		if err != nil {
			http.Error(w, "transport", 502)
			return
		}
		var response struct {
			Status int    `json:"status"`
			Body   string `json:"body"`
		}
		if json.Unmarshal(out, &response) != nil {
			http.Error(w, "decode", 502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" && dropNext.CompareAndSwap(true, false) {
			http.Error(w, "disposable lost response", 503)
			return
		}
		w.WriteHeader(response.Status)
		fmt.Fprint(w, response.Body)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestUpstreamDockerLifecycle(t *testing.T) {
	container := os.Getenv("HYPERHIVE_TEST_NPM_CONTAINER")
	if container == "" {
		t.Skip("requires disposable exact-image fixture")
	}
	upstreamTestDB(t)
	runtime := DockerUpstreamRuntime{Container: container}
	imageID, err := runtime.imageIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	upstreamCoordinator.runtime = runtime
	var dropNext atomic.Bool
	server := disposableNPMGateway(t, runtime, &dropNext)
	token := os.Getenv("HYPERHIVE_TEST_NPM_TOKEN")
	ctx := WithUpstreamNPMContext(context.Background(), server.URL, token)
	httpPool := Upstream{Name: "http_pool", Context: "http", Method: "round_robin", Members: []UpstreamMember{{IP: "127.0.0.1", Port: 18080}, {IP: "::1", Port: 18081}}}
	httpPool, err = SaveUpstream(ctx, httpPool, 0)
	if err != nil {
		t.Fatal(err)
	}
	streamPool := Upstream{Name: "stream_pool", Context: "stream", Method: "round_robin", Members: []UpstreamMember{{IP: "127.0.0.1", Port: 19090}, {IP: "::1", Port: 19091}}}
	streamPool, err = SaveUpstream(ctx, streamPool, 0)
	if err != nil {
		t.Fatal(err)
	}
	httpID, _ := json.Marshal(httpPool.ID)
	streamID, _ := json.Marshal(streamPool.ID)
	snippet := "proxy_set_header X-Preserved yes;"
	proxy := Proxy{DomainNames: []string{"pool.test"}, ForwardScheme: "http", UpstreamID: httpID, AccessListID: "0", Meta: map[string]any{}, AdvancedConfig: snippet, AllowWebsocketUpgrade: true, Locations: []Location{{Path: "/custom/", ForwardScheme: "http", UpstreamID: httpID, AdvancedConfig: &snippet}}}
	proxyID, err := CreateProxy(server.URL, token, proxy)
	if err != nil {
		t.Fatal(err)
	}
	proxy.DomainNames = []string{"shared.test"}
	proxy.Locations = []Location{}
	secondProxyID, err := CreateProxy(server.URL, token, proxy)
	if err != nil {
		t.Fatal(err)
	}
	stream := Stream{Incoming_port: 16060, Tcp_forwarding: true, Udp_forwarding: true, UpstreamID: streamID, Meta: map[string]any{"fixture_preserved": "stream"}}
	streamNPMID, err := CreateStream(server.URL, token, stream)
	if err != nil {
		t.Fatal(err)
	}
	assertTraffic := func(protocol, path string, expected string) {
		t.Helper()
		var code string
		switch protocol {
		case "http":
			code = fmt.Sprintf(`require('node:http').get({host:'127.0.0.1',path:%q,headers:{Host:'pool.test'}},r=>{let b='';r.on('data',v=>b+=v);r.on('end',()=>console.log(b))}).setTimeout(2000,()=>process.exit(1))`, path)
		case "tcp":
			code = `const s=require('node:net').connect(16060,'127.0.0.1');s.on('data',b=>console.log(b.toString()));s.setTimeout(2000,()=>process.exit(1));`
		case "udp":
			code = `const s=require('node:dgram').createSocket('udp4');s.on('message',b=>{console.log(b.toString());s.close()});s.send('probe',16060,'127.0.0.1');setTimeout(()=>process.exit(1),2000).unref();`
		}
		out, err := runtime.command(ctx, "exec", container, "node", "-e", code)
		if err != nil {
			t.Fatal(err)
		}
		label := strings.TrimSpace(string(out))
		if expected != "" && label != expected || expected == "" && label != "member-1" && label != "member-2" {
			t.Fatalf("%s %s reached %q", protocol, path, label)
		}
	}
	for _, path := range []string{"/", "/custom/"} {
		assertTraffic("http", path, "")
	}
	assertTraffic("tcp", "", "")
	assertTraffic("udp", "", "")
	// Custom location URIs and WebSocket upgrades survive pool translation.
	out, err := runtime.node(ctx, `require('node:http').get({host:'127.0.0.1',path:'/custom/path?x=1',headers:{Host:'pool.test'},agent:false},r=>{r.resume();r.on('end',()=>console.log(r.headers['x-request-path']))}).on('error',()=>process.exit(1))`, nil)
	if err != nil || strings.TrimSpace(string(out)) != "/custom/path?x=1" {
		t.Fatalf("custom location path changed: %s %v", out, err)
	}
	for _, path := range []string{"/ws", "/custom/ws"} {
		out, err := runtime.node(ctx, `const p=JSON.parse(require('node:fs').readFileSync(0,'utf8'));const s=require('node:net').connect(80,'127.0.0.1');let response='';s.on('connect',()=>s.write('GET '+p.path+' HTTP/1.1\r\nHost: pool.test\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n'));s.on('data',b=>response+=b.toString());s.on('end',()=>{if(!response.includes('101 Switching Protocols')||!response.includes('member-'))process.exit(1);console.log('passed')});s.setTimeout(2000,()=>process.exit(1));`, map[string]string{"path": path})
		if err != nil || strings.TrimSpace(string(out)) != "passed" {
			t.Fatalf("WebSocket %s: %s %v", path, out, err)
		}
	}
	// The backend rejects TLS without pool.test SNI; its self-signed certificate
	// is separate from any public-facing NPM certificate.
	tlsPool := Upstream{Name: "tls_pool", Context: "http", Method: "round_robin", Members: []UpstreamMember{{IP: "127.0.0.1", Port: 18443}, {IP: "::1", Port: 18444}}}
	tlsPool, err = SaveUpstream(ctx, tlsPool, 0)
	if err != nil {
		t.Fatal(err)
	}
	proxiesForTLS, err := GetAllProxys(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range proxiesForTLS {
		if p.ID == proxyID {
			p.ForwardScheme = "https"
			p.UpstreamID, _ = json.Marshal(tlsPool.ID)
			if err := EditProxy(server.URL, token, p); err != nil {
				t.Fatal(err)
			}
		}
	}
	assertTraffic("http", "/", "")
	proxiesForTLS, err = GetAllProxys(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range proxiesForTLS {
		if p.ID == proxyID {
			p.ForwardScheme = "http"
			p.UpstreamID = httpID
			if err := EditProxy(server.URL, token, p); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := DeleteUpstream(ctx, tlsPool.ID, tlsPool.Revision); err != nil {
		t.Fatal(err)
	}
	// Passive HTTP/TCP failover and explicit all-unavailable behavior use only
	// idempotent probes. UDP samples are separate datagrams, never replays.
	for i := range httpPool.Members {
		httpPool.Members[i].Settings = "max_fails=1 fail_timeout=1s"
	}
	httpPool, err = SaveUpstream(ctx, httpPool, httpPool.Revision)
	if err != nil {
		t.Fatal(err)
	}
	for i := range streamPool.Members {
		streamPool.Members[i].Settings = "max_fails=1 fail_timeout=1s"
	}
	streamPool, err = SaveUpstream(ctx, streamPool, streamPool.Revision)
	if err != nil {
		t.Fatal(err)
	}
	control := func(action string, member int) {
		t.Helper()
		_, err := runtime.node(ctx, `const p=JSON.parse(require('node:fs').readFileSync(0,'utf8'));fetch('http://127.0.0.1:19999/'+p.action+'/'+p.member).then(r=>{if(!r.ok)process.exit(1)}).catch(()=>process.exit(1))`, map[string]any{"action": action, "member": member})
		if err != nil {
			t.Fatal(err)
		}
	}
	control("stop", 1)
	for i := 0; i < 4; i++ {
		assertTraffic("http", "/", "member-2")
		assertTraffic("tcp", "", "member-2")
	}
	udpLossSample, err := runtime.node(ctx, `(async()=>{const dgram=require('node:dgram');let received=0;for(let i=0;i<12;i++){const result=await new Promise(resolve=>{const s=dgram.createSocket('udp4');const timeout=setTimeout(()=>{s.close();resolve('timeout')},200);s.on('message',b=>{clearTimeout(timeout);s.close();resolve(b.toString())});s.send('independent-probe-'+i,16060,'127.0.0.1')});if(result==='member-2')received++;else if(result!=='timeout')throw Error('unexpected response')}if(!received)throw Error('no UDP survivor response');console.log('passed')})().catch(()=>process.exit(1))`, nil)
	if err != nil || strings.TrimSpace(string(udpLossSample)) != "passed" {
		t.Fatal("UDP survivor sampling failed")
	}
	control("stop", 2)
	out, err = runtime.node(ctx, `require('node:http').get({host:'127.0.0.1',headers:{Host:'pool.test'},agent:false},r=>{r.resume();r.on('end',()=>console.log(r.statusCode))}).on('error',()=>process.exit(1))`, nil)
	if err != nil || strings.TrimSpace(string(out)) != "502" {
		t.Fatalf("all-unavailable HTTP did not return 502: %s %v", out, err)
	}
	control("start", 1)
	control("start", 2)
	time.Sleep(2200 * time.Millisecond)
	assertTraffic("http", "/", "")
	assertTraffic("tcp", "", "")
	assertTraffic("udp", "", "")
	got, err := GetUpstream(ctx, httpPool.ID)
	if err != nil || len(got.References) != 3 {
		t.Fatalf("references: %+v %v", got, err)
	}
	if err := DeleteUpstream(ctx, httpPool.ID, httpPool.Revision); err == nil {
		t.Fatal("referenced deletion accepted")
	}
	// A downstream POST may succeed while its HTTP response is lost. Do not retry;
	// restart reconciliation locates the unique journal marker and completes it.
	dropNext.Store(true)
	_, uncertainErr := CreateProxy(server.URL, token, Proxy{DomainNames: []string{"uncertain.test"}, ForwardScheme: "http", UpstreamID: httpID, AccessListID: "0", Meta: map[string]any{}, Locations: []Location{}})
	if uncertainErr == nil {
		t.Fatal("lost create response reported success")
	}
	if err := InitializeUpstreams(ctx); err != nil {
		t.Fatalf("uncertain create recovery failed: %v", err)
	}
	remaining, err := GetAllProxys(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	completedID := 0
	for _, p := range remaining {
		for _, domain := range p.DomainNames {
			if domain == "uncertain.test" {
				completedID = p.ID
				if string(p.UpstreamID) != string(httpID) {
					t.Fatal("confirmed create lost its binding")
				}
			}
		}
	}
	if completedID == 0 {
		t.Fatal("confirmed create was deleted during recovery")
	}
	if err := DeleteProxy(server.URL, token, completedID); err != nil {
		t.Fatal(err)
	} // Disposable fixture cleanup, not an MCP capability.
	// Renaming preserves internal references; one membership edit affects both hosts.
	httpPool.Name = "renamed_http"
	httpPool.Members = httpPool.Members[1:]
	httpPool, err = SaveUpstream(ctx, httpPool, httpPool.Revision)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/custom/"} {
		assertTraffic("http", path, "member-2")
	}
	streamPool.Members = streamPool.Members[1:]
	streamPool, err = SaveUpstream(ctx, streamPool, streamPool.Revision)
	if err != nil {
		t.Fatal(err)
	}
	assertTraffic("tcp", "", "member-2")
	assertTraffic("udp", "", "member-2")
	ps, err := GetAllProxys(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	var editing Proxy
	for _, p := range ps {
		if p.ID == proxyID {
			editing = p
		}
	}
	if string(editing.UpstreamID) != string(httpID) || editing.ForwardHost != "" || editing.AdvancedConfig != snippet || editing.Locations[0].AdvancedConfig == nil || *editing.Locations[0].AdvancedConfig != snippet {
		t.Fatal("proxy read lost user configuration or leaked internal names")
	}
	editing.UpstreamID = nil
	editing.AdvancedConfig += "\nproxy_read_timeout 61s;"
	if err := EditProxy(server.URL, token, editing); err != nil {
		t.Fatalf("omitted binding not preserved: %v", err)
	}
	if err := DisableProxy(server.URL, token, proxyID); err != nil {
		t.Fatal(err)
	}
	if err := DeleteUpstream(ctx, httpPool.ID, httpPool.Revision); err == nil {
		t.Fatal("disabled reference ignored")
	}
	if err := EnableProxy(server.URL, token, proxyID); err != nil {
		t.Fatal(err)
	}
	if err := InitializeUpstreams(ctx); err != nil {
		t.Fatal(err)
	}
	assertTraffic("http", "/", "member-2")
	assertTraffic("udp", "", "member-2")
	// A real ENOSPC failure on a staged file cannot replace the active generation.
	var next int64
	if err := db.DB.QueryRow("SELECT COALESCE(MAX(id),0)+1 FROM npm_upstream_operations").Scan(&next); err != nil {
		t.Fatal(err)
	}
	faultPath := fmt.Sprintf("%s/generations/%d/http.conf.hh-tmp", upstreamConfigRoot, next*2)
	if _, err := runtime.node(ctx, `const fs=require('node:fs');const p=JSON.parse(fs.readFileSync(0,'utf8'));fs.mkdirSync(require('node:path').dirname(p.path),{recursive:true});fs.symlinkSync('/dev/full',p.path)`, map[string]string{"path": faultPath}); err != nil {
		t.Fatal(err)
	}
	failedPool := httpPool
	failedPool.Name = "disk_full_must_not_apply"
	if _, err := SaveUpstream(ctx, failedPool, httpPool.Revision); err == nil {
		t.Fatal("disk-full write reported success")
	}
	assertTraffic("http", "/", "member-2")
	// Preserve invalid user configuration; fail closed until that hook is repaired.
	hook, err := runtime.read(ctx, "/data/nginx/custom/http_top.conf")
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.write(ctx, "/data/nginx/custom/http_top.conf", hook+"\nhh_invalid_directive on;\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveUpstream(ctx, failedPool, httpPool.Revision); err == nil {
		t.Fatal("invalid external configuration reported success")
	}
	assertTraffic("http", "/", "member-2")
	if err := runtime.write(ctx, "/data/nginx/custom/http_top.conf", hook); err != nil {
		t.Fatal(err)
	}
	if err := InitializeUpstreams(ctx); err != nil {
		t.Fatalf("external configuration recovery failed: %v", err)
	}
	// Reject reloads while allowing nginx -t, then rehearse restart recovery.
	if _, err := runtime.node(ctx, `const fs=require('node:fs');fs.renameSync('/usr/sbin/nginx','/usr/sbin/nginx.hh-real');fs.writeFileSync('/usr/sbin/nginx','#!/bin/sh\nif [ "$1" = "-s" ]; then exit 1; fi\nexec /usr/sbin/nginx.hh-real "$@"\n',{mode:0o755});`, nil); err != nil {
		t.Fatal(err)
	}
	restoreBinary := func() {
		_, _ = runtime.node(context.Background(), `const fs=require('node:fs');if(fs.existsSync('/usr/sbin/nginx.hh-real'))fs.renameSync('/usr/sbin/nginx.hh-real','/usr/sbin/nginx');`, nil)
	}
	defer restoreBinary()
	if _, err := SaveUpstream(ctx, failedPool, httpPool.Revision); err == nil {
		t.Fatal("reload failure reported success")
	}
	assertTraffic("http", "/", "member-2")
	restoreBinary()
	if err := InitializeUpstreams(ctx); err != nil {
		t.Fatalf("reload recovery failed: %v", err)
	}
	// Unsupported template edits are refused before any managed installation.
	template, err := runtime.read(ctx, "/app/templates/_location.conf")
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.write(ctx, "/app/templates/_location.conf", template+"\n# external template change\n"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Verify(ctx); err == nil {
		t.Fatal("template mismatch accepted")
	}
	if err := runtime.write(ctx, "/app/templates/_location.conf", template); err != nil {
		t.Fatal(err)
	}
	// Recreate this disposable container with the generated compose override.
	// This uses its existing anonymous data volumes, never the user's NPM container.
	inspect, err := runtime.command(ctx, "inspect", "--format", "{{json .Mounts}}", container)
	if err != nil {
		t.Fatal(err)
	}
	var mounts []struct{ Destination, Name string }
	if json.Unmarshal(inspect, &mounts) != nil {
		t.Fatal("mount inspection")
	}
	dataVolume, sslVolume := "", ""
	for _, m := range mounts {
		if m.Destination == "/data" {
			dataVolume = m.Name
		}
		if m.Destination == "/etc/letsencrypt" {
			sslVolume = m.Name
		}
	}
	if dataVolume == "" || sslVolume == "" {
		t.Fatal("missing isolated volumes")
	}
	work := t.TempDir()
	fixtureDir, err := filepath.Abs("../internal/testnpm")
	if err != nil {
		t.Fatal(err)
	}
	compose := fmt.Sprintf("services:\n  app:\n    image: %s\n    container_name: %s\n    network_mode: none\n    volumes:\n      - data:/data\n      - ssl:/etc/letsencrypt\n      - %q\nvolumes:\n  data:\n    external: true\n    name: %s\n  ssl:\n    external: true\n    name: %s\n", imageID, container, fixtureDir+":/fixture:ro", dataVolume, sslVolume)
	composePath := filepath.Join(work, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(compose), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMPOSE_PROJECT_NAME", container)
	args, err := prepareUpstreamComposeForContainer(ctx, work, container)
	if err != nil {
		t.Fatal(err)
	}
	// An image mismatch cannot overwrite the existing generated adapter.
	if err := os.WriteFile(composePath, []byte(strings.Replace(compose, imageID, "hh-unverified-test-image:never", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareUpstreamComposeForContainer(ctx, work, container); err == nil {
		t.Fatal("unverified compose image accepted")
	}
	if err := os.WriteFile(composePath, []byte(compose), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.command(ctx, "rm", "-f", container); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = work
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compose recreation: %v %.1000s", err, output)
	}
	ready := false
	for i := 0; i < 60; i++ {
		if _, err := runtime.node(ctx, `fetch('http://127.0.0.1:81/api').then(r=>{if(!r.ok)process.exit(1)}).catch(()=>process.exit(1))`, nil); err == nil {
			ready = true
			break
		}
		time.Sleep(time.Second)
	}
	if !ready {
		t.Fatal("recreated NPM did not become ready")
	}
	if _, err := runtime.command(ctx, "exec", "-d", container, "node", "/fixture/backends.cjs"); err != nil {
		t.Fatal(err)
	}
	if err := InitializeUpstreams(ctx); err != nil {
		t.Fatalf("container recreation reconciliation: %v", err)
	}
	assertTraffic("http", "/", "member-2")
	assertTraffic("tcp", "", "member-2")
	assertTraffic("udp", "", "member-2")
	if current, err := runtime.read(ctx, "/data/nginx/custom/stream.conf"); err != nil || !strings.Contains(current, "existing user stream configuration") {
		t.Fatal("user stream hook was overwritten")
	}
	// An out-of-band edit is observable and cannot be overwritten by a pool change.
	actual, err := readUpstreamConsumer(server.URL, token, "proxy", proxyID)
	if err != nil {
		t.Fatal(err)
	}
	actual.Proxy.AdvancedConfig += "\nproxy_connect_timeout 7s;"
	if err := rawEditProxy(server.URL, token, *actual.Proxy); err != nil {
		t.Fatal(err)
	}
	httpPool.Name = "must_not_apply"
	if _, err := SaveUpstream(ctx, httpPool, httpPool.Revision); err == nil {
		t.Fatal("drift overwritten")
	}
	// Restore exact last managed payload only within this disposable fixture.
	bindings, err := db.ListNPMUpstreamBindings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bindings {
		if b.Consumer == "proxy" && b.ID == proxyID {
			var old upstreamConsumer
			_ = json.Unmarshal(b.Expected, &old)
			if err := rawEditProxy(server.URL, token, *old.Proxy); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	ps, err = GetAllProxys(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if p.ID == proxyID {
			p.UpstreamID = json.RawMessage("null")
			p.ForwardHost = "127.0.0.1"
			p.ForwardPort = 18080
			p.Locations[0].UpstreamID = json.RawMessage("null")
			p.Locations[0].ForwardHost = "127.0.0.1"
			p.Locations[0].ForwardPort = 18080
			if err := EditProxy(server.URL, token, p); err != nil {
				t.Fatal(err)
			}
		}
	}
	assertTraffic("http", "/", "member-1")
	assertTraffic("http", "/custom/", "member-1")
	if err := DeleteProxy(server.URL, token, secondProxyID); err != nil {
		t.Fatal(err)
	}
	if err := DeleteUpstream(ctx, httpPool.ID, httpPool.Revision); err != nil {
		t.Fatal(err)
	}
	if err := DeleteStream(server.URL, token, streamNPMID); err != nil {
		t.Fatal(err)
	}
	if err := DeleteUpstream(ctx, streamPool.ID, streamPool.Revision); err != nil {
		t.Fatal(err)
	}
	if err := DeleteProxy(server.URL, token, proxyID); err != nil {
		t.Fatal(err)
	}
}
