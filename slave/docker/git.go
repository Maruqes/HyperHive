package docker

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slave/extra"

	proto "github.com/Maruqes/512SvMan/api/proto/extra"
)

const allGitDir string = "docker_git"

type Git struct{}

var our_git *Git

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

	// Scanner for stdout
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			extra.SendWebsocketMessage(line, extraS, msgType)
		}
	}()

	// Scanner for stderr
	go func() {
		stderrScanner := bufio.NewScanner(stderr)
		for stderrScanner.Scan() {
			line := stderrScanner.Text()
			extra.SendWebsocketMessage(line, extraS, msgType)
		}
	}()

	// Wait for command to finish
	if err := cmd.Wait(); err != nil {
		return err
	}

	return nil
}

func ExecWithSocketAndEnv(ctx context.Context, msgType proto.WebSocketsMessageType, extraS string, envVars map[string]string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)

	// Set environment variables
	for key, value := range envVars {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}
	// Preserve existing environment variables
	cmd.Env = append(cmd.Env, os.Environ()...)

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

	// Scanner for stdout
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			extra.SendWebsocketMessage(line, extraS, msgType)
		}
	}()

	// Scanner for stderr
	go func() {
		stderrScanner := bufio.NewScanner(stderr)
		for stderrScanner.Scan() {
			line := stderrScanner.Text()
			extra.SendWebsocketMessage(line, extraS, msgType)
		}
	}()

	// Wait for command to finish
	if err := cmd.Wait(); err != nil {
		return err
	}

	return nil
}

func (g *Git) GitClone(ctx context.Context, link, folderToRun, name, id string, envVars map[string]string) error {
	gitFolder := name

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

	// Make sure the directory exists
	if err := os.MkdirAll(allGitDir, os.ModePerm); err != nil {
		return err
	}

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
	return string(output), nil
}

func (g *Git) GitList(ctx context.Context) (*GitList, error) {
	//get all folders inside allGitDir, get all repo link and folder name return
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

func (g *Git) GitRemove(ctx context.Context, name, folderToRun string, id string) error {
	for _, char := range name {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')) {
			return fmt.Errorf("invalid name: %s; only letters and numbers are allowed", name)
		}
	}

	folderPath := filepath.Join(allGitDir, name)

	// Try to run docker compose down, ignore errors
	composeFile := filepath.Join(folderPath, folderToRun)
	ExecWithSocket(ctx, proto.WebSocketsMessageType_DockerCompose, id, "docker", "compose", "-f", composeFile, "down")

	if err := os.RemoveAll(folderPath); err != nil {
		return err
	}
	return nil
}

func (g *Git) GitUpdate(ctx context.Context, name string, folderToRun string, id string, envVars map[string]string) error {
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
