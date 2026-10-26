package supportdeploy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	support "hyperhive/support"
)

const containerName = "hyperhive-support"

type runner func(context.Context, ...string) ([]byte, error)

func Ensure(ctx context.Context) error {
	return ensure(ctx, os.Getenv, os.Setenv, func(ctx context.Context, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, "docker", args...).Output()
	})
}

func ensure(ctx context.Context, getenv func(string) string, setenv func(string, string) error, run runner) error {
	if strings.EqualFold(getenv("SUPPORT_AUTO_DEPLOY"), "false") {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	upstream := getenv("SUPPORT_URL")
	if upstream == "" {
		upstream = "http://127.0.0.1:8091"
	}
	listen, err := managedListen(upstream)
	if err != nil {
		return err
	}
	stateDir := getenv("SUPPORT_STATE_DIR")
	if stateDir == "" {
		stateDir = "/opt/hyperhive/support"
	}
	if err := privateDirectory(stateDir); err != nil {
		return err
	}
	existing, err := checkOwnership(ctx, run)
	if err != nil {
		return err
	}
	if getenv("SUPPORT_PROXY_SECRET") == "" {
		if _, err := os.Stat(filepath.Join(stateDir, "proxy-secret")); errors.Is(err, os.ErrNotExist) {
			output, err := run(ctx, "volume", "ls", "--filter", "name=hyperhive-support-data", "--format", "{{.Name}}")
			if err != nil {
				return errors.New("could not inspect support data volume")
			}
			for _, name := range strings.Fields(string(output)) {
				existing = existing || name == "hyperhive-support-data"
			}
			if existing {
				return errors.New("existing support deployment has no persisted proxy secret; restore proxy-secret or configure SUPPORT_PROXY_SECRET explicitly")
			}
		}
	}
	secret, err := loadSecret(stateDir, getenv("SUPPORT_PROXY_SECRET"))
	if err != nil {
		return err
	}
	contextDir, err := os.MkdirTemp("", "hyperhive-support-build-")
	if err != nil {
		return errors.New("could not prepare support build directory")
	}
	defer os.RemoveAll(contextDir)
	image, err := extractSources(contextDir)
	if err != nil {
		return err
	}
	if _, err = run(ctx, "image", "inspect", image); err != nil {
		if _, err = run(ctx, "build", "--network", "host", "--tag", image, contextDir); err != nil {
			return errors.New("support image build failed; existing container was not changed")
		}
	}
	if _, err = checkOwnership(ctx, run); err != nil {
		return err
	}
	envPath := filepath.Join(stateDir, "support.env")
	if err = privateWrite(envPath, []byte("SUPPORT_PROXY_SECRET="+secret+"\n")); err != nil {
		return err
	}
	compose := composeConfig(image, listen, envPath)
	composePath := filepath.Join(stateDir, "compose.json")
	if err = privateWrite(composePath, compose); err != nil {
		return err
	}
	if _, err = run(ctx, "compose", "--project-name", containerName, "--file", composePath, "up", "-d", "--pull", "never", "--wait", "--wait-timeout", "60"); err != nil {
		return errors.New("support container startup or healthcheck failed; persistent data was preserved")
	}
	if err = setenv("SUPPORT_URL", upstream); err != nil {
		return errors.New("could not configure support upstream")
	}
	if err = setenv("SUPPORT_PROXY_SECRET", secret); err != nil {
		return errors.New("could not configure support proxy secret")
	}
	return nil
}

func managedListen(upstream string) (string, error) {
	parsed, err := url.Parse(upstream)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("automatic support deployment requires a loopback HTTP SUPPORT_URL; disable SUPPORT_AUTO_DEPLOY for an external service")
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return "", errors.New("automatic support deployment requires an explicit SUPPORT_URL port")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", errors.New("invalid support upstream port")
	}
	address := net.ParseIP(host)
	if address == nil || !address.IsLoopback() {
		return "", errors.New("automatic support deployment requires a loopback IP; disable SUPPORT_AUTO_DEPLOY for an external service")
	}
	return net.JoinHostPort(host, port), nil
}

func privateDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return errors.New("could not create support state directory")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("support state directory must be a real directory")
	}
	if err := os.Chmod(path, 0700); err != nil {
		return errors.New("could not protect support state directory")
	}
	return nil
}

func privateWrite(path string, data []byte) error {
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return errors.New("support state file must be a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("could not inspect support state file")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".support-state-")
	if err != nil {
		return errors.New("could not write support state file")
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err = file.Write(data); err != nil {
		return errors.New("could not write support state file")
	}
	if err = file.Sync(); err != nil {
		return errors.New("could not persist support state file")
	}
	if err = file.Close(); err != nil {
		return errors.New("could not close support state file")
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return errors.New("could not replace support state file")
	}
	return nil
}

func loadSecret(directory, configured string) (string, error) {
	path := filepath.Join(directory, "proxy-secret")
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return "", errors.New("support proxy secret must be a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("could not inspect support proxy secret")
	}
	secret := configured
	if secret == "" {
		data, err := os.ReadFile(path)
		if err == nil {
			secret = string(data)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", errors.New("could not read support proxy secret")
		} else {
			var token [32]byte
			if _, err = rand.Read(token[:]); err != nil {
				return "", errors.New("could not generate support proxy secret")
			}
			secret = base64.RawURLEncoding.EncodeToString(token[:])
		}
	}
	if len(secret) < 32 || strings.ContainsAny(secret, "\r\n\x00") {
		return "", errors.New("support proxy secret must contain at least 32 bytes and no line breaks or NUL")
	}
	if err := privateWrite(path, []byte(secret)); err != nil {
		return "", err
	}
	return secret, nil
}

func checkOwnership(ctx context.Context, run runner) (bool, error) {
	output, err := run(ctx, "container", "ls", "--all", "--filter", "name=^/"+containerName+"$", "--format", "{{.ID}}")
	if err != nil {
		return false, errors.New("could not inspect support containers; verify Docker is available")
	}
	if strings.TrimSpace(string(output)) == "" {
		return false, nil
	}
	output, err = run(ctx, "container", "inspect", "--format", "{{json .Config.Labels}}", containerName)
	if err != nil {
		return true, errors.New("could not verify existing support container ownership")
	}
	var labels map[string]string
	if json.Unmarshal(output, &labels) != nil || labels["io.hyperhive.support.managed"] != "true" || labels["com.docker.compose.project"] != containerName || labels["com.docker.compose.service"] != containerName {
		return true, errors.New("refusing to replace an existing support container without HyperHive management and Compose ownership labels")
	}
	return true, nil
}

func extractSources(directory string) (string, error) {
	hash := sha256.New()
	err := fs.WalkDir(support.DeploymentFiles, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := support.DeploymentFiles.ReadFile(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%d:%s:%d:", len(path), path, len(data))
		hash.Write(data)
		target := filepath.Join(directory, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0600)
	})
	if err != nil {
		return "", errors.New("could not extract embedded support source")
	}
	return fmt.Sprintf("hyperhive-support:%x", hash.Sum(nil)[:16]), nil
}

func composeConfig(image, listen, envPath string) []byte {
	config := map[string]any{"services": map[string]any{containerName: map[string]any{
		"image": image, "container_name": containerName, "network_mode": "host", "restart": "unless-stopped",
		"env_file": []map[string]string{{"path": strings.ReplaceAll(envPath, "$", "$$"), "format": "raw"}}, "environment": map[string]string{"SUPPORT_LISTEN": listen, "SUPPORT_DB_PATH": "/data/support.db"},
		"volumes": []string{"hyperhive-support-data:/data"}, "read_only": true, "cap_drop": []string{"ALL"}, "security_opt": []string{"no-new-privileges:true"},
		"labels": map[string]string{"io.hyperhive.support.managed": "true"},
	}}, "volumes": map[string]any{"hyperhive-support-data": map[string]string{"name": "hyperhive-support-data"}}}
	data, _ := json.MarshalIndent(config, "", "  ")
	return data
}
