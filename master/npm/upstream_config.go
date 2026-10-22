package npm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Docker image config ID; this is not a registry manifest digest for repo@sha256.
const UpstreamImageDigest = "sha256:5c80aaf5e26c1f45a6ce9f58ad00edceabb717d68236ff5b06d6f4d1f1271742"
const upstreamConfigRoot = "/data/nginx/custom/hyperhive"

//go:embed upstream_templates/*.conf
var upstreamTemplates embed.FS

// NPMUpgradeManifest exports the compiled adapter to the offline update script.
// The script checks Target against the source pin to reject an outdated master.
func NPMUpgradeManifest() map[string]any {
	files := map[string]map[string]string{}
	for name, path := range upstreamTemplatePaths {
		original, _ := upstreamTemplates.ReadFile("upstream_templates/" + name)
		files[name] = map[string]string{"path": path, "original": string(original), "patched": patchedUpstreamTemplate(name, string(original))}
	}
	return map[string]any{"target": image, "target_id": UpstreamImageDigest, "templates": files}
}

func (d DockerUpstreamRuntime) imageIdentity(ctx context.Context) (string, error) {
	b, err := d.command(ctx, "inspect", "--format", "{{.Image}}", d.Container)
	return strings.TrimSpace(string(b)), err
}

var upstreamTemplatePaths = map[string]string{
	"proxy_host.conf": "/app/templates/proxy_host.conf",
	"_location.conf":  "/app/templates/_location.conf",
	"stream.conf":     "/app/templates/stream.conf",
	"proxy.conf":      "/etc/nginx/conf.d/include/proxy.conf",
}

type UpstreamRuntime interface {
	Apply(context.Context, int64, []Upstream) error
	Verify(context.Context) error
}

// DockerUpstreamRuntime never discovers remote hosts or changes NPM's database.
type DockerUpstreamRuntime struct{ Container string }

func (d DockerUpstreamRuntime) command(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("NPM container operation failed: %w", err)
	}
	return out, nil
}

func (d DockerUpstreamRuntime) read(ctx context.Context, path string) (string, error) {
	b, err := d.command(ctx, "exec", d.Container, "cat", path)
	return string(b), err
}

func (d DockerUpstreamRuntime) readOptional(ctx context.Context, path string) (string, error) {
	data, err := d.node(ctx, `const fs=require('node:fs');const p=JSON.parse(fs.readFileSync(0,'utf8'));try{process.stdout.write(fs.readFileSync(p.path,'utf8'))}catch(e){if(e.code!=='ENOENT')throw e}`, map[string]string{"path": path})
	return string(data), err
}

func (d DockerUpstreamRuntime) node(ctx context.Context, code string, input any) ([]byte, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", d.Container, "node", "-e", code)
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("NPM file operation failed: %w", err)
	}
	return out, nil
}

func (d DockerUpstreamRuntime) write(ctx context.Context, path, content string) error {
	_, err := d.node(ctx, `const fs=require('node:fs');const p=JSON.parse(fs.readFileSync(0,'utf8'));fs.mkdirSync(require('node:path').dirname(p.path),{recursive:true});const f=p.path+'.hh-tmp';fs.writeFileSync(f,p.content,{mode:0o644});const fd=fs.openSync(f,'r');try{fs.fsyncSync(fd)}finally{fs.closeSync(fd)};fs.renameSync(f,p.path);const dir=fs.openSync(require('node:path').dirname(p.path),'r');try{fs.fsyncSync(dir)}finally{fs.closeSync(dir)};`, map[string]string{"path": path, "content": content})
	return err
}

// NPM's IPv6 startup hook may append trailing blank lines to the common include.
// Only that file permits trailing LF normalization; directive bytes remain pinned.
func matchesUpstreamTemplate(name, actual, original string) bool {
	patched := patchedUpstreamTemplate(name, original)
	if name == "proxy.conf" {
		actual = strings.TrimRight(actual, "\n")
		original = strings.TrimRight(original, "\n")
		patched = strings.TrimRight(patched, "\n")
	}
	return actual == original || actual == patched
}

func patchedUpstreamTemplate(name, original string) string {
	tls := `{% assign hh_prefix = forward_host | slice: 0, 12 %}{% if hh_prefix == "hh_upstream_" %}{% unless advanced_config contains "proxy_ssl_" %}
    # HyperHive backend TLS: SNI follows the public host; certificate verification is explicit.
    proxy_ssl_server_name on;
    proxy_ssl_name $host;
    proxy_ssl_verify off;
{% endunless %}{% endif %}`
	switch name {
	case "proxy_host.conf":
		return strings.Replace(original, "    # Proxy!", tls+"\n    # Proxy!", 1)
	case "proxy.conf":
		return strings.Replace(original, "proxy_pass       $forward_scheme://$server:$port$request_uri;", "set $hh_target \"$server:$port\";\nif ($server ~ \"^hh_upstream_[0-9]+$\") { set $hh_target $server; }\nproxy_pass $forward_scheme://$hh_target$request_uri;", 1)
	case "_location.conf":
		result := strings.Replace(original, "{{ forward_host }}:{{ forward_port }}", `{{ forward_host }}{% assign hh_prefix = forward_host | slice: 0, 12 %}{% unless hh_prefix == "hh_upstream_" %}:{{ forward_port }}{% endunless %}`, 1)
		return strings.Replace(result, "    proxy_pass", tls+"\n    proxy_pass", 1)
	case "stream.conf":
		return strings.ReplaceAll(original, "{{ forwarding_host }}:{{ forwarding_port }}", `{{ forwarding_host }}{% assign hh_prefix = forwarding_host | slice: 0, 12 %}{% unless hh_prefix == "hh_upstream_" %}:{{ forwarding_port }}{% endunless %}`)
	default:
		return original
	}
}

func (d DockerUpstreamRuntime) Verify(ctx context.Context) error {
	identity, err := d.imageIdentity(ctx)
	if err != nil {
		return err
	}
	if identity != UpstreamImageDigest {
		return fmt.Errorf("unsupported NPM image; exact verified image identity is required")
	}
	main, err := d.read(ctx, "/etc/nginx/nginx.conf")
	if err != nil {
		return err
	}
	for _, hook := range []string{"http_top", "stream"} {
		if strings.Count(main, "/data/nginx/custom/"+hook+"[.]conf") != 1 {
			return fmt.Errorf("unsupported NPM include layout")
		}
	}
	for name, path := range upstreamTemplatePaths {
		original, err := upstreamTemplates.ReadFile("upstream_templates/" + name)
		if err != nil {
			return err
		}
		actual, err := d.read(ctx, path)
		if err != nil {
			return err
		}
		if !matchesUpstreamTemplate(name, actual, string(original)) {
			sum := sha256.Sum256([]byte(actual))
			return fmt.Errorf("unsupported NPM template %s (sha256 %s)", name, hex.EncodeToString(sum[:]))
		}
	}
	return nil
}

// Apply validates an entire candidate configuration before switching one symlink.
// Callers retain the old snapshot and invoke Apply again for compensation.
func (d DockerUpstreamRuntime) Apply(ctx context.Context, generation int64, pools []Upstream) error {
	if err := d.Verify(ctx); err != nil {
		return err
	}
	base := upstreamConfigRoot + "/generations/" + strconv.FormatInt(generation, 10)
	http := RenderUpstreams(pools, "http") + fmt.Sprintf("server { listen unix:/tmp/hyperhive-upstream-status.sock; server_name _; location = /generation { return 200 \"%d\"; } }\n", generation)
	if err := d.write(ctx, base+"/http.conf", http); err != nil {
		return err
	}
	if err := d.write(ctx, base+"/stream.conf", RenderUpstreams(pools, "stream")); err != nil {
		return err
	}
	main, err := d.read(ctx, "/etc/nginx/nginx.conf")
	if err != nil {
		return err
	}
	for _, hook := range []string{"http_top", "stream"} {
		path := "/data/nginx/custom/" + hook + ".conf"
		// A missing optional NPM hook is equivalent to an empty file.
		existing, err := d.readOptional(ctx, path)
		if err != nil {
			return err
		}
		include := fmt.Sprintf("include %s/current/%s.conf;", upstreamConfigRoot, map[string]string{"http_top": "http", "stream": "stream"}[hook])
		without := strings.ReplaceAll(existing, include, "# HyperHive upstream include staged below")
		candidate := without + "\ninclude " + base + "/" + map[string]string{"http_top": "http", "stream": "stream"}[hook] + ".conf;\n"
		if err := d.write(ctx, base+"/"+hook+"_hook.conf", candidate); err != nil {
			return err
		}
		main = strings.ReplaceAll(main, "/data/nginx/custom/"+hook+"[.]conf", base+"/"+hook+"_hook.conf")
	}
	candidateMain := "/etc/nginx/hyperhive-candidate-" + strconv.FormatInt(generation, 10) + ".conf"
	if err := d.write(ctx, candidateMain, main); err != nil {
		return err
	}
	if _, err := d.command(ctx, "exec", d.Container, "nginx", "-t", "-p", "/etc/nginx/", "-c", candidateMain); err != nil {
		return fmt.Errorf("candidate configuration rejected: %w", err)
	}
	// Install only known, version-pinned templates. Runtime changes survive master
	// restart through reconciliation; container recreation reinstalls these before mutations.
	for name, path := range upstreamTemplatePaths {
		original, _ := upstreamTemplates.ReadFile("upstream_templates/" + name)
		patched := patchedUpstreamTemplate(name, string(original))
		actual, err := d.read(ctx, path)
		if err != nil {
			return err
		}
		if actual == patched {
			continue
		}
		if err := d.write(ctx, path, patched); err != nil {
			return err
		}
	}
	_, err = d.node(ctx, `const fs=require('node:fs');const p=JSON.parse(fs.readFileSync(0,'utf8'));const link=p.root+'/current';const tmp=link+'.next';try{fs.unlinkSync(tmp)}catch(e){if(e.code!=='ENOENT')throw e}fs.symlinkSync(p.target,tmp);fs.renameSync(tmp,link);const fd=fs.openSync(p.root,'r');try{fs.fsyncSync(fd)}finally{fs.closeSync(fd)};`, map[string]string{"root": upstreamConfigRoot, "target": base})
	if err != nil {
		return err
	}
	for _, hook := range []string{"http_top", "stream"} {
		path := "/data/nginx/custom/" + hook + ".conf"
		existing, err := d.readOptional(ctx, path)
		if err != nil {
			return err
		}
		include := fmt.Sprintf("include %s/current/%s.conf;", upstreamConfigRoot, map[string]string{"http_top": "http", "stream": "stream"}[hook])
		if !strings.Contains(existing, include) {
			if err := d.write(ctx, path, existing+"\n# HyperHive managed upstream pools\n"+include+"\n"); err != nil {
				return err
			}
		}
	}
	if _, err := d.command(ctx, "exec", d.Container, "nginx", "-t"); err != nil {
		return err
	}
	if _, err := d.command(ctx, "exec", d.Container, "nginx", "-s", "reload"); err != nil {
		return err
	}
	// Reading a generated file cannot establish activation. Probe a generation-specific
	// response served by the new workers over a container-local Unix socket.
	for attempt := 0; attempt < 30; attempt++ {
		out, err := d.node(ctx, `const http=require('node:http');http.get({socketPath:'/tmp/hyperhive-upstream-status.sock',path:'/generation'},r=>{let b='';r.on('data',v=>b+=v);r.on('end',()=>process.stdout.write(b))}).on('error',()=>process.exit(1)).setTimeout(1000,()=>process.exit(1));`, nil)
		if err == nil && strings.TrimSpace(string(out)) == strconv.FormatInt(generation, 10) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("NGINX generation activation could not be verified")
}

// UpstreamTemplateFingerprints is useful for diagnostics without exposing source or credentials.
func UpstreamTemplateFingerprints() map[string]string {
	result := map[string]string{}
	for name, path := range upstreamTemplatePaths {
		b, _ := upstreamTemplates.ReadFile(filepath.ToSlash("upstream_templates/" + name))
		sum := sha256.Sum256(b)
		result[path] = hex.EncodeToString(sum[:])
	}
	return result
}
