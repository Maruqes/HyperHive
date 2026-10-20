package npm

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func exampleUpstream() Upstream {
	return Upstream{Name: "app_pool", Context: "http", Method: "round_robin", Members: []UpstreamMember{{IP: "127.0.0.1", Port: 8080, Settings: "max_fails=3 weight=2 fail_timeout=10s"}}}
}

func TestNormalizeUpstream(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Upstream)
		want   string
	}{
		{"valid", func(p *Upstream) { p.Name = " APP_POOL " }, ""},
		{"reserved", func(p *Upstream) { p.Name = "hh_upstream_42" }, "name"},
		{"injection", func(p *Upstream) { p.Members[0].Settings = "weight=1; down" }, "characters"},
		{"dns", func(p *Upstream) { p.Members[0].IP = "backend.test" }, "IP literal"},
		{"duplicate", func(p *Upstream) { p.Members = append(p.Members, p.Members[0]) }, "duplicate endpoint"},
		{"ipv6 duplicate", func(p *Upstream) {
			p.Members[0].IP = "::1"
			p.Members = append(p.Members, UpstreamMember{IP: "0:0:0:0:0:0:0:1", Port: 8080})
		}, "duplicate endpoint"},
		{"down", func(p *Upstream) { p.Members[0].Settings = "down" }, "usable primary"},
		{"backup hash", func(p *Upstream) {
			p.Method = "hash"
			p.Options.HashKey = "$remote_addr"
			p.Members[0].Settings = "backup"
		}, "incompatible"},
		{"duplicate setting", func(p *Upstream) { p.Members[0].Settings = "weight=1 weight=2" }, "duplicate"},
		{"zero weight", func(p *Upstream) { p.Members[0].Settings = "weight=0" }, "range"},
		{"invalid duration", func(p *Upstream) { p.Members[0].Settings = "fail_timeout=10x" }, "duration"},
		{"hash injection", func(p *Upstream) { p.Method = "hash"; p.Options.HashKey = "$remote_addr; down" }, "hash_key"},
		{"stream ip hash", func(p *Upstream) { p.Context = "stream"; p.Method = "ip_hash" }, "unsupported"},
		{"unknown", func(p *Upstream) { p.Method = "least_time" }, "unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := exampleUpstream()
			tc.change(&p)
			err := NormalizeUpstream(&p)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				if p.Name != "app_pool" || p.Members[0].Settings != "weight=2 max_fails=3 fail_timeout=10s" {
					t.Fatalf("not canonical: %+v", p)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func FuzzServerSettings(f *testing.F) {
	for _, s := range []string{"", "weight=2 max_fails=3 fail_timeout=10s", "down", "weight=1;}", "backup"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out, _, err := normalizeServerSettings(s, "round_robin")
		if err == nil {
			again, _, err := normalizeServerSettings(out, "round_robin")
			if err != nil || again != out {
				t.Fatalf("not canonical: %q", out)
			}
			if strings.ContainsAny(out, ";{}#\r\n$") {
				t.Fatal("unsafe output")
			}
		}
	})
}

func TestUpstreamDockerRuntime(t *testing.T) {
	container := os.Getenv("HYPERHIVE_TEST_NPM_CONTAINER")
	if container == "" {
		t.Skip("run with the disposable exact-image compatibility fixture")
	}
	runtime := DockerUpstreamRuntime{Container: container}
	pools := []Upstream{
		{ID: 1, Name: "http_pool", Context: "http", Method: "round_robin", Members: []UpstreamMember{{IP: "127.0.0.1", Port: 18080}, {IP: "::1", Port: 18081}}},
		{ID: 2, Name: "stream_pool", Context: "stream", Method: "round_robin", Members: []UpstreamMember{{IP: "127.0.0.1", Port: 19090}, {IP: "::1", Port: 19091}}},
	}
	for index, method := range []string{"round_robin", "least_conn", "random", "random_two", "hash", "hash_consistent", "ip_hash"} {
		candidate := append([]Upstream{}, pools...)
		candidate[0].Method = method
		if method != "ip_hash" {
			candidate[1].Method = method
		}
		if strings.HasPrefix(method, "hash") {
			candidate[0].Options.HashKey = "$remote_addr"
			candidate[1].Options.HashKey = "$remote_addr"
		}
		t.Run(method, func(t *testing.T) {
			for i := range candidate {
				if err := NormalizeUpstream(&candidate[i]); err != nil {
					t.Fatal(err)
				}
			}
			if err := runtime.Apply(context.Background(), int64(100+index), candidate); err != nil {
				t.Fatal(err)
			}
			labels := probeDisposableProtocols(t, runtime, 32)
			for protocol, values := range labels {
				seen := map[string]bool{}
				for _, value := range values {
					if value != "member-1" && value != "member-2" {
						t.Fatalf("%s returned %q", protocol, value)
					}
					seen[value] = true
				}
				affinity := strings.HasPrefix(method, "hash") || method == "ip_hash" && strings.HasPrefix(protocol, "http")
				if affinity && len(seen) != 1 || !affinity && len(seen) != 2 {
					t.Fatalf("%s %s routing: %v", method, protocol, seen)
				}
			}
			if strings.HasPrefix(method, "hash") || method == "ip_hash" {
				out, err := runtime.node(context.Background(), `(async()=>{const http=require('node:http');const get=localAddress=>new Promise((resolve,reject)=>http.get({host:'127.0.0.1',localAddress,path:'/',headers:{Host:'pool.test'},agent:false},r=>{let b='';r.on('data',v=>b+=v);r.on('end',()=>resolve(b))}).on('error',reject));const seen=new Set();for(let i=1;i<=12;i++){const values=[];for(let j=0;j<4;j++)values.push(await get('127.0.'+i+'.1'));if(new Set(values).size!==1)throw Error('client affinity changed');seen.add(values[0])}if(seen.size!==2)throw Error('controlled clients never reached both members');console.log('passed')})().catch(e=>{console.error(e.message);process.exit(1)})`, nil)
				if err != nil || strings.TrimSpace(string(out)) != "passed" {
					t.Fatalf("controlled-client affinity failed: %v %s", err, out)
				}
			}
			if method == "least_conn" || method == "random_two" {
				out, err := runtime.node(context.Background(), `(async()=>{const http=require('node:http');const get=(path)=>new Promise((resolve,reject)=>{const req=http.get({host:'127.0.0.1',path,headers:{Host:'pool.test'},agent:false},resolve);req.on('error',reject)});const held=await get('/hold');const label=held.headers['x-member'];for(let i=0;i<8;i++){const r=await get('/');let b='';for await(const c of r)b+=c;if(b===label)throw Error('least_conn reused the busy member')}held.destroy();console.log('passed')})().catch(e=>{console.error(e.message);process.exit(1)})`, nil)
				if err != nil || strings.TrimSpace(string(out)) != "passed" {
					t.Fatalf("held-connection selection failed: %v %s", err, out)
				}
			}
		})
	}
	weighted := append([]Upstream{}, pools...)
	for i := range weighted {
		weighted[i].Members = append([]UpstreamMember{}, pools[i].Members...)
		weighted[i].Members[0].Settings = "weight=5 max_conns=1000 max_fails=2 fail_timeout=1s"
	}
	if err := runtime.Apply(context.Background(), 900, weighted); err != nil {
		t.Fatal(err)
	}
	for protocol, values := range probeDisposableProtocols(t, runtime, 120) {
		primary := 0
		for _, value := range values {
			if value == "member-1" {
				primary++
			}
		}
		if primary < 90 || primary > 110 {
			t.Fatalf("%s weighted distribution: %d/120", protocol, primary)
		}
	}
	if err := runtime.Apply(context.Background(), 999, pools); err != nil {
		t.Fatal(err)
	}
}

func probeDisposableProtocols(t *testing.T, runtime DockerUpstreamRuntime, count int) map[string][]string {
	t.Helper()
	out, err := runtime.node(context.Background(), `(async()=>{const fs=require('node:fs'),http=require('node:http'),net=require('node:net'),dgram=require('node:dgram');const p=JSON.parse(fs.readFileSync(0,'utf8'));const results={};
 const httpProbe=path=>new Promise((resolve,reject)=>{http.get({host:'127.0.0.1',path,headers:{Host:'pool.test'},agent:false},r=>{let b='';r.on('data',v=>b+=v);r.on('end',()=>resolve(b))}).on('error',reject).setTimeout(1500,()=>reject(Error('HTTP timeout')))});
 const tcpProbe=()=>new Promise((resolve,reject)=>{const s=net.connect(16060,'127.0.0.1');let b='';s.on('data',v=>b+=v);s.on('end',()=>resolve(b));s.on('error',reject);s.setTimeout(1500,()=>{s.destroy();reject(Error('TCP timeout'))})});
 const udpProbe=()=>new Promise((resolve,reject)=>{const s=dgram.createSocket('udp4');const timer=setTimeout(()=>{s.close();reject(Error('UDP timeout'))},1500);s.on('message',b=>{clearTimeout(timer);s.close();resolve(b.toString())});s.on('error',reject);s.send('probe',16060,'127.0.0.1')});
 for(const [protocol,probe]of [['http',()=>httpProbe('/')],['http_location',()=>httpProbe('/custom/')],['tcp',tcpProbe],['udp',udpProbe]]){results[protocol]=[];for(let i=0;i<p.count;i++)results[protocol].push(await probe())}console.log(JSON.stringify(results))})().catch(e=>{console.error(e.message);process.exit(1)})`, map[string]int{"count": count})
	if err != nil {
		t.Fatalf("traffic sampling failed: %v %s", err, out)
	}
	var results map[string][]string
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatal(err)
	}
	return results
}

func TestTemplateStartupNewlines(t *testing.T) {
	original, _ := upstreamTemplates.ReadFile("upstream_templates/proxy.conf")
	for _, actual := range []string{string(original), string(original) + "\n\n", patchedUpstreamTemplate("proxy.conf", string(original)) + "\n"} {
		if !matchesUpstreamTemplate("proxy.conf", actual, string(original)) {
			t.Fatal("startup newline rejected")
		}
	}
	if matchesUpstreamTemplate("proxy.conf", string(original)+"# modified\n", string(original)) {
		t.Fatal("modified content accepted")
	}
	if matchesUpstreamTemplate("stream.conf", "original\n", "original") {
		t.Fatal("other template normalized")
	}
}
