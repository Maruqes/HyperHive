package docker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slave/extra"
	"strings"
	"sync"

	proto "github.com/Maruqes/512SvMan/api/proto/extra"
)

const allGitDir string = "docker_git"

const maxScanTokenSize = 1024 * 1024 // 1MiB per line

type Git struct{}

var our_git *Git

func ensureAllGitDir() error {
	return os.MkdirAll(allGitDir, os.ModePerm)
}

func scanAndSend(r io.Reader, extraS string, msgType proto.WebSocketsMessageType, capture *strings.Builder, captureMu *sync.Mutex, wg *sync.WaitGroup) {
	defer wg.Done()

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024), maxScanTokenSize)
	for scanner.Scan() {
		line := scanner.Text()
		extra.SendWebsocketMessage(line, extraS, msgType)
		if capture != nil {
			captureMu.Lock()
			capture.WriteString(line)
			capture.WriteString("\n")
			captureMu.Unlock()
		}
	}
	// Se houver erro de scan (ex: linha > maxScanTokenSize), reporta via websocket e captura.
	if err := scanner.Err(); err != nil {
		errLine := fmt.Sprintf("scanner error: %v", err)
		extra.SendWebsocketMessage(errLine, extraS, msgType)
		if capture != nil {
			captureMu.Lock()
			capture.WriteString(errLine)
			capture.WriteString("\n")
			captureMu.Unlock()
		}
	}
}

func ExecWithSocket(ctx context.Context, msgType proto.WebSocketsMessageType, extraS, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	var wg sync.WaitGroup
	var stderrCapture strings.Builder
	var captureMu sync.Mutex

	wg.Add(2)
	go scanAndSend(stdout, extraS, msgType, nil, &captureMu, &wg)
	go scanAndSend(stderr, extraS, msgType, &stderrCapture, &captureMu, &wg)

	cmdErr := cmd.Wait()
	wg.Wait()

	if cmdErr != nil {
		errS := strings.TrimSpace(stderrCapture.String())
		if errS != "" {
			return fmt.Errorf("%s + %s", cmdErr.Error(), errS)
		}
		return cmdErr
	}
	return nil
}

func ExecWithSocketAndEnv(ctx context.Context, msgType proto.WebSocketsMessageType, extraS string, envVars map[string]string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)

	// Preserve existing environment variables first, then override/add custom ones.
	cmd.Env = append(cmd.Env, os.Environ()...)
	for key, value := range envVars {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	var wg sync.WaitGroup
	var stderrCapture strings.Builder
	var captureMu sync.Mutex

	wg.Add(2)
	go scanAndSend(stdout, extraS, msgType, nil, &captureMu, &wg)
	go scanAndSend(stderr, extraS, msgType, &stderrCapture, &captureMu, &wg)

	cmdErr := cmd.Wait()
	wg.Wait()

	if cmdErr != nil {
		errS := strings.TrimSpace(stderrCapture.String())
		if errS != "" {
			return fmt.Errorf("%s + %s", cmdErr.Error(), errS)
		}
		return cmdErr
	}
	return nil
}

func (g *Git) GitClone(ctx context.Context, link, folderToRun, name, id string, envVars map[string]string) error {
	gitFolder := name

	// Make sure the directory exists
	if err := os.MkdirAll(allGitDir, os.ModePerm); err != nil {
		return err
	}

	// Check if name is only letters and numbers
	for _, char := range name {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')) {
			return fmt.Errorf("invalid name: %s; only letters and numbers are allowed", name)
		}
	}

	// Check if folder already exists in GitList
	gitList, err := g.GitList(ctx)
	if err != nil {
		return err
	}
	for _, elem := range gitList.Elems {
		if elem.Name == name {
			return fmt.Errorf("folder with name %s already exists", name)
		}
	}

	cloneDir := filepath.Join(allGitDir, gitFolder)

	// Execute git clone command
	if err := ExecWithSocket(ctx, proto.WebSocketsMessageType_DockerCompose, id, "git", "clone", link, cloneDir); err != nil {
		return err
	}

	composeFile := filepath.Join(cloneDir, folderToRun)

	// Execute docker compose build with env vars
	if err := ExecWithSocketAndEnv(ctx, proto.WebSocketsMessageType_DockerCompose, id, envVars, "docker", "compose", "-f", composeFile, "build"); err != nil {
		return err
	}

	// Execute docker compose up with env vars
	return ExecWithSocketAndEnv(ctx, proto.WebSocketsMessageType_DockerCompose, id, envVars, "docker", "compose", "-f", composeFile, "up", "-d")
}

type GitList struct {
	Elems []struct {
		Name     string
		RepoLink string
	}
}

func getRepoLink(path string) (string, error) {
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	cmd.Dir = path
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (g *Git) GitList(ctx context.Context) (*GitList, error) {
	//get all folders inside allGitDir, get all repo link and folder name return
	_ = ctx
	if err := ensureAllGitDir(); err != nil {
		return nil, err
	}

	dirs, err := os.ReadDir(allGitDir)
	if err != nil {
		return nil, err
	}
	var res GitList
	for _, dir := range dirs {
		if dir.IsDir() {
			repo, err := getRepoLink(filepath.Join(allGitDir, dir.Name()))
			if err != nil {
				continue
			}
			res.Elems = append(res.Elems, struct {
				Name     string
				RepoLink string
			}{Name: dir.Name(), RepoLink: repo})
		}
	}

	return &res, nil
}

func (g *Git) GitRemove(ctx context.Context, name, folderToRun string, id string, envVars map[string]string) error {
	_ = ctx
	if err := ensureAllGitDir(); err != nil {
		return err
	}
	for _, char := range name {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')) {
			return fmt.Errorf("invalid name: %s; only letters and numbers are allowed", name)
		}
	}

	if folderToRun == "" {
		folderPath := filepath.Join(allGitDir, name)
		return os.RemoveAll(folderPath)
	}

	folderPath := filepath.Join(allGitDir, name)
	composeFile := filepath.Join(folderPath, folderToRun)

	if err := ExecWithSocketAndEnv(ctx, proto.WebSocketsMessageType_DockerCompose, id, envVars, "docker", "compose", "-f", composeFile, "down"); err != nil {
		return fmt.Errorf("docker compose down: %w", err)
	}

	if err := os.RemoveAll(folderPath); err != nil {
		return err
	}
	return nil
}

func (g *Git) GitUpdate(ctx context.Context, name string, folderToRun string, id string, envVars map[string]string) error {
	_ = ctx
	if err := ensureAllGitDir(); err != nil {
		return err
	}
	for _, char := range name {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')) {
			return fmt.Errorf("invalid name: %s; only letters and numbers are allowed", name)
		}
	}

	repoPath := filepath.Join(allGitDir, name)

	// Execute git pull
	if err := ExecWithSocket(ctx, proto.WebSocketsMessageType_DockerCompose, id, "git", "-C", repoPath, "pull"); err != nil {
		return err
	}

	composeFile := filepath.Join(repoPath, folderToRun)

	// Execute docker compose build with env vars
	if err := ExecWithSocketAndEnv(ctx, proto.WebSocketsMessageType_DockerCompose, id, envVars, "docker", "compose", "-f", composeFile, "build"); err != nil {
		return err
	}

	// Execute docker compose up with env vars
	return ExecWithSocketAndEnv(ctx, proto.WebSocketsMessageType_DockerCompose, id, envVars, "docker", "compose", "-f", composeFile, "up", "-d")
}
