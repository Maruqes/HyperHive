package services

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "github.com/Maruqes/512SvMan/api/proto/celld"
)

const siloImage = "docker.io/pgsty/silo:RELEASE.2026-09-03T13-18-01Z"

var siloName = "hyperhive-silo"

const siloData = "/var/lib/hyperhive/silo"

//go:embed celld_bootstrap/*
var celldBootstrap embed.FS

func celldCommand(ctx context.Context, input string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	out, err := cmd.CombinedOutput()
	// Arguments can contain credentials; never include the command in an error.
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func ensureSiloContainer(ctx context.Context, st celldFleetState) error {
	setup, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	out, err := celldCommand(setup, "", "docker", "container", "ls", "-a", "--filter", "name=^/"+siloName+"$", "--format", "{{.Names}}")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) == "" {
		if err := os.MkdirAll(siloData, 0700); err != nil {
			return err
		}
		envFile := "/var/lib/hyperhive/silo-admin.env"
		if err := os.WriteFile(envFile, []byte("MINIO_ROOT_USER="+st.RootUser+"\nMINIO_ROOT_PASSWORD="+st.RootPassword+"\n"), 0600); err != nil {
			return err
		}
		if _, err = celldCommand(setup, "", "docker", "run", "-d", "--name", siloName, "--restart", "unless-stopped",
			"--env-file", envFile, "-p", net.JoinHostPort(st.MasterIP, "9000")+":9000", "-v", siloData+":/data:Z",
			siloImage, "server", "/data", "--console-address", ":9001"); err != nil {
			return err
		}
	} else {
		out, err = celldCommand(setup, "", "docker", "inspect", siloName)
		if err != nil {
			return err
		}
		var info []struct {
			Config     struct{ Image string }
			State      struct{ Running bool }
			Mounts     []struct{ Source, Destination string }
			HostConfig struct {
				PortBindings map[string][]struct{ HostIP, HostPort string }
			}
		}
		if err = json.Unmarshal(out, &info); err != nil {
			return err
		}
		if len(info) != 1 || info[0].Config.Image != siloImage {
			return fmt.Errorf("existing SILO container has an unexpected image; refusing to replace it")
		}
		mounted := false
		for _, m := range info[0].Mounts {
			if m.Source == siloData && m.Destination == "/data" {
				mounted = true
			}
		}
		bindings := info[0].HostConfig.PortBindings["9000/tcp"]
		if !mounted || len(bindings) != 1 || bindings[0].HostIP != st.MasterIP || bindings[0].HostPort != "9000" {
			return fmt.Errorf("existing SILO container has unexpected storage or network bindings")
		}
		if !info[0].State.Running {
			if _, err = celldCommand(setup, "", "docker", "start", siloName); err != nil {
				return err
			}
		}
	}
	ready, stop := context.WithTimeout(setup, 60*time.Second)
	defer stop()
	for {
		req, err := http.NewRequestWithContext(ready, "GET", "http://"+net.JoinHostPort(st.MasterIP, "9000")+"/minio/health/ready", nil)
		if err != nil {
			return err
		}
		resp, err := celldHTTP.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ready.Done():
			timer.Stop()
			return fmt.Errorf("SILO readiness: %w", ready.Err())
		case <-timer.C:
		}
	}
}

func siloCLI(ctx context.Context, st celldFleetState, bucket, access, secret, input string, args ...string) error {
	_, err := celldCommand(ctx, input, "docker", append([]string{"exec", "-i", siloName, "mcli", "--config-dir", "/tmp/hyperhive-mcli-" + bucket}, args...)...)
	if err == nil {
		return nil
	}
	message := err.Error()
	for _, value := range []string{st.RootUser, st.RootPassword, access, secret} {
		if value != "" {
			message = strings.ReplaceAll(message, value, "[redacted]")
		}
	}
	return fmt.Errorf("SILO provisioning: %s", message)
}

func ensureCelldStorage(ctx context.Context, st celldFleetState, f *celldRecord) error {
	setup, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	mcli := func(input string, args ...string) error {
		return siloCLI(setup, st, f.Storage.Bucket, f.AccessKey, f.SecretKey, input, args...)
	}
	if err := mcli("", "alias", "set", "local", "http://127.0.0.1:9000", st.RootUser, st.RootPassword); err != nil {
		return err
	}
	if err := mcli("", "mb", "--ignore-existing", "local/"+f.Storage.Bucket); err != nil {
		return err
	}
	policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:*"],"Resource":["arn:aws:s3:::%s","arn:aws:s3:::%s/*"]}]}`, f.Storage.Bucket, f.Storage.Bucket)
	if err := mcli(policy, "admin", "policy", "create", "local", f.Storage.Bucket, "/dev/stdin"); err != nil {
		return err
	}
	if err := mcli("", "admin", "user", "add", "local", f.AccessKey, f.SecretKey); err != nil {
		return err
	}
	return mcli("", "admin", "policy", "attach", "local", f.Storage.Bucket, "--user", f.AccessKey)
}

func removeCelldStorage(ctx context.Context, st celldFleetState, bucket, access string) error {
	setup, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	mcli := func(args ...string) error { return siloCLI(setup, st, bucket, access, "", "", args...) }
	if err := mcli("alias", "set", "local", "http://127.0.0.1:9000", st.RootUser, st.RootPassword); err != nil {
		return err
	}
	// A retry may find either resource already removed by an earlier attempt.
	absent := func(err error) error {
		if err == nil {
			return nil
		}
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "does not exist") || strings.Contains(message, "nosuchbucket") || strings.Contains(message, "nosuchuser") || strings.Contains(message, "nosuchpolicy") {
			return nil
		}
		return err
	}
	if err := absent(mcli("rm", "--recursive", "--force", "local/"+bucket)); err != nil {
		return err
	}
	if err := absent(mcli("rb", "local/"+bucket)); err != nil {
		return err
	}
	if access != "" {
		if err := absent(mcli("admin", "user", "remove", "local", access)); err != nil {
			return err
		}
	}
	return absent(mcli("admin", "policy", "remove", "local", bucket))
}

func observeCelldStorage(ctx context.Context, cfg *pb.Config) CelldStorage {
	result := CelldStorage{CheckedAt: time.Now().UTC()}
	r, err := celldS3(ctx, cfg, "GET", "", nil, "")
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if r.code != 200 {
		result.Error = fmt.Sprintf("list SILO buckets: HTTP %d", r.code)
		return result
	}
	var listing struct {
		XMLName               xml.Name                `xml:"ListAllMyBucketsResult"`
		Buckets               []struct{ Name string } `xml:"Buckets>Bucket"`
		IsTruncated           bool
		ContinuationToken     string
		NextContinuationToken string
	}
	if err = xml.Unmarshal([]byte(r.body), &listing); err != nil {
		result.Error = err.Error()
		return result
	}
	if listing.IsTruncated || listing.ContinuationToken != "" || listing.NextContinuationToken != "" {
		result.Error = "SILO bucket listing is incomplete"
		return result
	}
	result.Buckets = []string{}
	for _, b := range listing.Buckets {
		result.Buckets = append(result.Buckets, b.Name)
	}
	sort.Strings(result.Buckets)
	count := len(result.Buckets)
	result.BucketCount = &count
	result.Healthy = true
	return result
}

type celldS3Response struct {
	code   int
	header http.Header
	body   string
}

func celldS3(ctx context.Context, cfg *pb.Config, method, key string, headers map[string]string, body string) (*celldS3Response, error) {
	// curl already supplies SigV4; credentials enter on stdin, never in process arguments.
	var config strings.Builder
	line := func(k, v string) { fmt.Fprintf(&config, "%s = %s\n", k, strconv.Quote(v)) }
	bucket := strings.TrimPrefix(cfg.Bucket, "s3://")
	target := cfg.Endpoint + "/"
	if bucket != "" {
		target += bucket + "/"
	}
	line("url", target+key)
	line("aws-sigv4", "aws:amz:"+cfg.Region+":s3")
	line("user", cfg.AccessKey+":"+cfg.SecretKey)
	line("request", method)
	line("header", "Expect:")
	for k, v := range headers {
		line("header", k+": "+v)
	}
	if body != "" {
		line("data-binary", body)
	}
	cmd := exec.CommandContext(ctx, "curl", "--silent", "--show-error", "--http1.1", "--raw", "--include", "--noproxy", "*", "--max-time", "10", "--config", "-")
	cmd.Stdin = strings.NewReader(config.String())
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("SILO request failed: %w", err)
	}
	req, err := http.NewRequest(method, cfg.Endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(out)), req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 2<<20 {
		return nil, fmt.Errorf("SILO response exceeds limit")
	}
	return &celldS3Response{code: resp.StatusCode, header: resp.Header, body: string(data)}, nil
}

func validateCelldStorage(ctx context.Context, cfg *pb.Config) error {
	id, err := celldSecret()
	if err != nil {
		return err
	}
	key := "probe/hyperhive-" + id
	defer func() { _, _ = celldS3(ctx, cfg, "DELETE", key, nil, "") }()
	put := func(condition, value, body string, expected int) (*celldS3Response, error) {
		r, err := celldS3(ctx, cfg, "PUT", key, map[string]string{condition: value}, body)
		if err != nil {
			return nil, err
		}
		if r.code != expected {
			return nil, fmt.Errorf("%s returned HTTP %d, expected %d", condition, r.code, expected)
		}
		return r, nil
	}
	first, err := put("If-None-Match", "*", "abcdefghij", 200)
	if err != nil {
		return err
	}
	etag := first.header.Get("ETag")
	if etag == "" {
		return fmt.Errorf("conditional create did not return an ETag")
	}
	if _, err = put("If-None-Match", "*", "wrong", 412); err != nil {
		return err
	}
	if _, err = put("If-Match", etag, "0123456789", 200); err != nil {
		return err
	}
	if _, err = put("If-Match", etag, "wrong", 412); err != nil {
		return err
	}
	r, err := celldS3(ctx, cfg, "GET", key, nil, "")
	if err != nil {
		return err
	}
	if r.code != 200 || r.body != "0123456789" {
		return fmt.Errorf("storage failed read-after-write consistency")
	}
	r, err = celldS3(ctx, cfg, "GET", key, map[string]string{"Range": "bytes=2-5"}, "")
	if err != nil {
		return err
	}
	if r.code != 206 || r.header.Get("Content-Range") != "bytes 2-5/10" || r.body != "2345" {
		return fmt.Errorf("storage failed exact ranged reads")
	}
	return nil
}

var celldToolsMu sync.Mutex

func bootstrapCelld(ctx context.Context, cfg *pb.Config) error {
	celldToolsMu.Lock()
	defer celldToolsMu.Unlock()
	r, err := celldS3(ctx, cfg, "GET", "deploy/current.json", nil, "")
	if err != nil {
		return err
	}
	if r.code == 200 {
		return nil
	}
	if r.code != 404 {
		return fmt.Errorf("read existing celld deployment: HTTP %d", r.code)
	}
	r, err = celldS3(ctx, cfg, "GET", "?list-type=2&max-keys=1000", nil, "")
	if err != nil {
		return err
	}
	if r.code != 200 {
		return fmt.Errorf("list bootstrap bucket: HTTP %d", r.code)
	}
	var listing struct {
		IsTruncated bool
		Contents    []struct{ Key string }
	}
	if err = xml.Unmarshal([]byte(r.body), &listing); err != nil {
		return err
	}
	if listing.IsTruncated {
		return fmt.Errorf("refusing to bootstrap a nonempty bucket without a deployment")
	}
	for _, object := range listing.Contents {
		if !strings.HasPrefix(object.Key, "probe/") {
			return fmt.Errorf("bucket already has data without a deployment; refusing to overwrite it")
		}
	}
	setup, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if _, err = exec.LookPath("node"); err != nil {
		if _, err = celldCommand(setup, "", "dnf", "install", "-y", "nodejs", "npm"); err != nil {
			return err
		}
	}
	if _, err = exec.LookPath("npm"); err != nil {
		if _, err = celldCommand(setup, "", "dnf", "install", "-y", "npm"); err != nil {
			return err
		}
	}
	tools := "/var/lib/hyperhive/celld-tools"
	if _, err = os.Stat(filepath.Join(tools, "node_modules/.bin/esbuild")); os.IsNotExist(err) {
		if _, err = celldCommand(setup, "", "npm", "install", "--prefix", tools, "--no-audit", "--no-fund", "esbuild@0.25.12"); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	project := "/var/lib/hyperhive/celld-bootstrap"
	if err = os.MkdirAll(project, 0700); err != nil {
		return err
	}
	files, err := celldBootstrap.ReadDir("celld_bootstrap")
	if err != nil {
		return err
	}
	for _, file := range files {
		data, err := celldBootstrap.ReadFile("celld_bootstrap/" + file.Name())
		if err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(project, file.Name()), data, 0600); err != nil {
			return err
		}
	}
	cmd := exec.CommandContext(setup, celldBinary, "deploy", project, "--bucket", cfg.Bucket, "--endpoint", cfg.Endpoint, "--region", cfg.Region)
	cmd.Env = append(os.Environ(), "AWS_ACCESS_KEY_ID="+cfg.AccessKey, "AWS_SECRET_ACCESS_KEY="+cfg.SecretKey, "AWS_REGION="+cfg.Region,
		"AWS_SESSION_TOKEN=",
		"PATH="+filepath.Join(tools, "node_modules/.bin")+":"+os.Getenv("PATH"))
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("deploy celld bootstrap: %w", err)
	}
	return nil
}
