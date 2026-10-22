package npm

import (
	"512SvMan/db"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// prepareUpstreamCompose adds a separate owned override; existing compose content
// and custom logging hooks stay untouched. Mounts survive container recreation.
func prepareUpstreamCompose(ctx context.Context, work string) ([]string, error) {
	return prepareUpstreamComposeForContainer(ctx, work, "npm")
}
func prepareUpstreamComposeForContainer(ctx context.Context, work, container string) ([]string, error) {
	args := []string{"compose"}
	if db.DB == nil {
		return append(args, "up", "-d"), nil
	}
	rows, err := db.ListNPMUpstreams(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return append(args, "up", "-d"), nil
	}
	// Inspect the image selected by the existing compose file before installing
	// overlays. A source default does not establish the deployed image identity.
	cmd := exec.CommandContext(ctx, "docker", "compose", "config", "--format", "json")
	cmd.Dir = work
	contentJSON, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("cannot inspect NPM compose configuration")
	}
	var config struct {
		Services map[string]struct {
			Image     string `json:"image"`
			Container string `json:"container_name"`
			Volumes   []struct {
				Source string `json:"source"`
				Target string `json:"target"`
			} `json:"volumes"`
		} `json:"services"`
	}
	if json.Unmarshal(contentJSON, &config) != nil || len(config.Services) != 1 {
		return nil, fmt.Errorf("upstream migration requires an unambiguous single-service NPM compose file")
	}
	serviceName, imageName := "", ""
	for name, service := range config.Services {
		if service.Container != container {
			return nil, fmt.Errorf("upstream migration requires the existing npm container")
		}
		for _, volume := range service.Volumes {
			for templateName, target := range upstreamTemplatePaths {
				if volume.Target == target && volume.Source != filepath.Join(work, "npm-data", "hyperhive-upstream-adapter", templateName) {
					return nil, fmt.Errorf("existing custom NPM template mount conflicts with the upstream adapter")
				}
			}
		}
		serviceName = name
		imageName = service.Image
	}
	identity, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", imageName).Output()
	if err != nil {
		return nil, fmt.Errorf("upstream adapter requires the verified NPM image digest")
	}
	if strings.TrimSpace(string(identity)) != UpstreamImageDigest {
		return nil, fmt.Errorf("upstream adapter requires the verified NPM image identity")
	}
	adapter := filepath.Join(work, "npm-data", "hyperhive-upstream-adapter")
	if err := os.MkdirAll(adapter, 0755); err != nil {
		return nil, err
	}
	var volumes strings.Builder
	for _, name := range []string{"proxy_host.conf", "_location.conf", "stream.conf", "proxy.conf"} {
		original, err := upstreamTemplates.ReadFile("upstream_templates/" + name)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(adapter, name)
		patched := patchedUpstreamTemplate(name, string(original))
		if existing, err := os.ReadFile(path); err == nil {
			actual, expected := string(existing), patched
			if name == "proxy.conf" {
				actual, expected = strings.TrimRight(actual, "\n"), strings.TrimRight(expected, "\n")
			}
			if actual != expected {
				return nil, fmt.Errorf("existing upstream adapter asset differs from the verified version")
			}
		} else if os.IsNotExist(err) {
			if err := os.WriteFile(path, []byte(patched), 0644); err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
		// Quoted YAML scalars safely retain spaces and punctuation in workspace paths.
		mode := ":ro"
		if name == "proxy.conf" {
			mode = ":rw"
		} // NPM IPv6 preparation rewrites every conf.d file.
		fmt.Fprintf(&volumes, "      - %s\n", strconv.Quote(path+":"+upstreamTemplatePaths[name]+mode))
	}
	content := "services:\n  " + strconv.Quote(serviceName) + ":\n    image: " + strconv.Quote(imageName) + "\n    volumes:\n" + volumes.String()
	path := filepath.Join(work, "docker-compose.hyperhive-upstreams.yml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return nil, err
	}
	return []string{"compose", "-f", "docker-compose.yml", "-f", "docker-compose.hyperhive-upstreams.yml", "up", "-d", "--pull", "never"}, nil
}
