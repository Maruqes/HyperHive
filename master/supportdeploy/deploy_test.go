package supportdeploy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fakeDocker struct {
	commands                           [][]string
	image, existing, volume, unmanaged bool
	failure                            string
}

func (docker *fakeDocker) run(ctx context.Context, args ...string) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	docker.commands = append(docker.commands, append([]string(nil), args...))
	command := strings.Join(args, " ")
	if docker.failure != "" && strings.HasPrefix(command, docker.failure) {
		return nil, errors.New("DO NOT LEAK secret-or-private-docker-output")
	}
	switch {
	case strings.HasPrefix(command, "container ls"):
		if docker.existing {
			return []byte("container-id\n"), nil
		}
	case strings.HasPrefix(command, "container inspect"):
		if docker.unmanaged {
			return []byte(`{"com.docker.compose.project":"other"}`), nil
		}
		return []byte(`{"io.hyperhive.support.managed":"true","com.docker.compose.project":"hyperhive-support","com.docker.compose.service":"hyperhive-support"}`), nil
	case strings.HasPrefix(command, "volume ls"):
		if docker.volume {
			return []byte("hyperhive-support-data\n"), nil
		}
	case strings.HasPrefix(command, "image inspect"):
		if !docker.image {
			return nil, errors.New("missing image")
		}
	case strings.HasPrefix(command, "build"):
		docker.image = true
	case strings.HasPrefix(command, "compose"):
		docker.existing = true
		docker.volume = true
	}
	return nil, nil
}

func environment(testing *testing.T) (map[string]string, func(string, string) error) {
	testing.Helper()
	env := map[string]string{"SUPPORT_STATE_DIR": filepath.Join(testing.TempDir(), "state")}
	return env, func(key, value string) error { env[key] = value; return nil }
}
func hasCommand(commands [][]string, prefix string) bool {
	for _, command := range commands {
		if strings.HasPrefix(strings.Join(command, " "), prefix) {
			return true
		}
	}
	return false
}

func TestEnsureCreatesProtectedPersistentConfiguration(testing *testing.T) {
	env, setenv := environment(testing)
	docker := &fakeDocker{}
	if err := ensure(context.Background(), func(key string) string { return env[key] }, setenv, docker.run); err != nil {
		testing.Fatal(err)
	}
	secret := env["SUPPORT_PROXY_SECRET"]
	if len(secret) < 32 || env["SUPPORT_URL"] != "http://127.0.0.1:8091" {
		testing.Fatal("resolved configuration missing")
	}
	for _, name := range []string{"proxy-secret", "support.env", "compose.json"} {
		info, err := os.Stat(filepath.Join(env["SUPPORT_STATE_DIR"], name))
		if err != nil || info.Mode().Perm() != 0600 {
			testing.Fatalf("%s permissions: %v %v", name, info, err)
		}
	}
	info, err := os.Stat(env["SUPPORT_STATE_DIR"])
	if err != nil || info.Mode().Perm() != 0700 {
		testing.Fatal("state directory permissions", err)
	}
	data, err := os.ReadFile(filepath.Join(env["SUPPORT_STATE_DIR"], "support.env"))
	if err != nil || string(data) != "SUPPORT_PROXY_SECRET="+secret+"\n" {
		testing.Fatal("env file did not preserve secret", err)
	}
	var config struct {
		Services map[string]struct {
			Image       string            `json:"image"`
			NetworkMode string            `json:"network_mode"`
			Restart     string            `json:"restart"`
			ReadOnly    bool              `json:"read_only"`
			Volumes     []string          `json:"volumes"`
			CapDrop     []string          `json:"cap_drop"`
			Security    []string          `json:"security_opt"`
			Environment map[string]string `json:"environment"`
		} `json:"services"`
		Volumes map[string]struct {
			Name string `json:"name"`
		} `json:"volumes"`
	}
	data, err = os.ReadFile(filepath.Join(env["SUPPORT_STATE_DIR"], "compose.json"))
	if err != nil {
		testing.Fatal(err)
	}
	if err = json.Unmarshal(data, &config); err != nil {
		testing.Fatal(err)
	}
	service := config.Services[containerName]
	if service.NetworkMode != "host" || service.Restart != "unless-stopped" || !service.ReadOnly || !reflect.DeepEqual(service.CapDrop, []string{"ALL"}) || !reflect.DeepEqual(service.Security, []string{"no-new-privileges:true"}) || !reflect.DeepEqual(service.Volumes, []string{"hyperhive-support-data:/data"}) || config.Volumes["hyperhive-support-data"].Name != "hyperhive-support-data" || service.Environment["SUPPORT_LISTEN"] != "127.0.0.1:8091" || service.Environment["SUPPORT_DB_PATH"] != "/data/support.db" {
		testing.Fatalf("unsafe deployment configuration: %+v", config)
	}
	if strings.Contains(string(data), secret) {
		testing.Fatal("compose should reference the protected env file")
	}
	if !hasCommand(docker.commands, "build --network host --tag "+service.Image) || !hasCommand(docker.commands, "compose --project-name hyperhive-support --file ") {
		testing.Fatal("build or compose missing", docker.commands)
	}
	for _, args := range docker.commands {
		if strings.Contains(strings.Join(args, " "), secret) {
			testing.Fatal("secret in command arguments")
		}
		if args[0] == "build" {
			if _, err := os.Stat(args[len(args)-1]); !errors.Is(err, os.ErrNotExist) {
				testing.Fatal("build context was not cleaned")
			}
		}
		if args[0] == "compose" && !strings.HasSuffix(strings.Join(args, " "), "up -d --pull never --wait --wait-timeout 60") {
			testing.Fatal("readiness or pull policy missing", args)
		}
	}
	docker.commands = nil
	env["SUPPORT_PROXY_SECRET"] = ""
	if err := ensure(context.Background(), func(key string) string { return env[key] }, setenv, docker.run); err != nil {
		testing.Fatal(err)
	}
	if env["SUPPORT_PROXY_SECRET"] != secret || hasCommand(docker.commands, "build") {
		testing.Fatal("restart changed secret or rebuilt unchanged image")
	}
	for _, args := range docker.commands {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "--force-recreate") || strings.Contains(joined, " down") || strings.Contains(joined, " rm") {
			testing.Fatal("destructive restart", args)
		}
	}
}

func TestComposePreservesConfiguredSecret(test *testing.T) {
	if err := exec.Command("docker", "compose", "version").Run(); err != nil {
		test.Skip("Docker Compose is unavailable")
	}
	directory := filepath.Join(test.TempDir(), "state-$UNEXPANDED")
	if err := privateDirectory(directory); err != nil {
		test.Fatal(err)
	}
	secret := strings.Repeat("x", 32) + " $UNEXPANDED # ' \\\" \\"
	envPath := filepath.Join(directory, "support.env")
	if err := privateWrite(envPath, []byte("SUPPORT_PROXY_SECRET="+secret+"\n")); err != nil {
		test.Fatal(err)
	}
	composePath := filepath.Join(directory, "compose.json")
	if err := privateWrite(composePath, composeConfig("hyperhive-support:test", "127.0.0.1:8091", envPath)); err != nil {
		test.Fatal(err)
	}
	output, err := exec.Command("docker", "compose", "--file", composePath, "config", "--format", "json").Output()
	if err != nil {
		test.Fatal("Compose could not parse deployment; requires Compose 2.30 or later")
	}
	var config struct {
		Services map[string]struct {
			Environment map[string]string `json:"environment"`
		} `json:"services"`
	}
	if json.Unmarshal(output, &config) != nil || config.Services[containerName].Environment["SUPPORT_PROXY_SECRET"] != strings.ReplaceAll(secret, "$", "$$") {
		test.Fatal("Compose changed the proxy secret")
	}
}

func TestManagedURLAndOptOut(testing *testing.T) {
	for _, upstream := range []string{"https://127.0.0.1:8091", "http://192.0.2.1:8091", "http://localhost:8091", "http://127.0.0.1", "http://127.0.0.1:0", "http://127.0.0.1:8091/support", "http://user:pass@127.0.0.1:8091", "http://127.0.0.1:8091?secret=value"} {
		if _, err := managedListen(upstream); err == nil {
			testing.Errorf("accepted invalid managed URL %s", upstream)
		}
	}
	if listen, err := managedListen("http://[::1]:8092"); err != nil || listen != "[::1]:8092" {
		testing.Fatal(listen, err)
	}
	env, setenv := environment(testing)
	env["SUPPORT_AUTO_DEPLOY"] = "false"
	env["SUPPORT_URL"] = "https://external.example"
	docker := &fakeDocker{}
	if err := ensure(context.Background(), func(key string) string { return env[key] }, setenv, docker.run); err != nil || len(docker.commands) != 0 || env["SUPPORT_URL"] != "https://external.example" {
		testing.Fatal("opt out changed configuration", err)
	}
}

func TestRefusesUnmanagedContainersAndLostSecrets(test *testing.T) {
	for _, testcase := range []struct {
		name     string
		docker   fakeDocker
		expected string
	}{
		{"unmanaged", fakeDocker{existing: true, unmanaged: true}, "ownership"},
		{"missing container secret", fakeDocker{existing: true}, "restore proxy-secret"},
		{"missing volume secret", fakeDocker{volume: true}, "restore proxy-secret"},
	} {
		test.Run(testcase.name, func(testing *testing.T) {
			env, setenv := environment(testing)
			docker := testcase.docker
			err := ensure(context.Background(), func(key string) string { return env[key] }, setenv, docker.run)
			if err == nil || !strings.Contains(err.Error(), testcase.expected) || hasCommand(docker.commands, "build") || hasCommand(docker.commands, "compose") {
				testing.Fatalf("ownership/recovery failure: %v %+v", err, docker.commands)
			}
		})
	}
}

func TestFailuresAreSanitizedAndBuildPrecedesReconciliation(test *testing.T) {
	for _, failure := range []string{"container ls", "volume ls", "build", "compose"} {
		test.Run(failure, func(testing *testing.T) {
			env, setenv := environment(testing)
			docker := &fakeDocker{failure: failure}
			err := ensure(context.Background(), func(key string) string { return env[key] }, setenv, docker.run)
			if err == nil || strings.Contains(err.Error(), "DO NOT LEAK") {
				testing.Fatal("unsanitized or missing failure", err)
			}
			if env["SUPPORT_PROXY_SECRET"] != "" || env["SUPPORT_URL"] != "" {
				testing.Fatal("published generated proxy credentials before readiness")
			}
			if failure == "build" && hasCommand(docker.commands, "compose") {
				testing.Fatal("reconciled after failed build")
			}
		})
	}
	env, setenv := environment(test)
	docker := &fakeDocker{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ensure(ctx, func(key string) string { return env[key] }, setenv, docker.run); err == nil {
		test.Fatal("canceled deployment succeeded")
	}
}

func TestEmbeddedSourcesDeterministicAndProtectedState(testing *testing.T) {
	first := testing.TempDir()
	second := testing.TempDir()
	image, err := extractSources(first)
	if err != nil {
		testing.Fatal(err)
	}
	repeated, err := extractSources(second)
	if err != nil || image != repeated {
		testing.Fatal("unstable image fingerprint", err)
	}
	for _, path := range []string{"Dockerfile", ".dockerignore", "go.mod", "go.sum", "cmd/hh-support/main.go", "internal/storage/sqlite/migrations/001_initial.sql"} {
		if _, err := os.Stat(filepath.Join(first, path)); err != nil {
			testing.Fatal("missing build source", path, err)
		}
	}
	file := filepath.Join(testing.TempDir(), "target")
	if err := os.WriteFile(file, []byte("unchanged"), 0600); err != nil {
		testing.Fatal(err)
	}
	link := filepath.Join(testing.TempDir(), "link")
	if err := os.Symlink(file, link); err != nil {
		testing.Fatal(err)
	}
	if err := privateWrite(link, []byte("bad")); err == nil {
		testing.Fatal("followed state symlink")
	}
	if data, _ := os.ReadFile(file); string(data) != "unchanged" {
		testing.Fatal("overwrote symlink target")
	}
	if _, err := loadSecret(testing.TempDir(), strings.Repeat("x", 32)+"\nINJECTED=value"); err == nil {
		testing.Fatal("accepted injected env value")
	}
}
