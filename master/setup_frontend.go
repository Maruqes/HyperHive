package main

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// URL do web-dist.zip (manténs a que já tens)
	CUR_LINK = "https://github.com/JotaBarbosaDev/HyperHive/releases/download/v1.0.1/web-dist.zip"

	// Onde vais extrair o frontend no host
	frontendDir = "/opt/hyperhive/frontend"

	// Nome do container Docker
	containerName = "hyperhive-frontend"

	// Porta do host -> container
	hostPort = "8079" // http://servidor:8089
)

// setupFrontendContainer faz:
// 1) download do ZIP
// 2) unzip para frontendDir
// 3) recria o container nginx a servir essa pasta
func setupFrontendContainer() error {
	fmt.Println("[1/3] A fazer download do web-dist.zip...")
	zipPath, err := downloadZip(CUR_LINK)
	if err != nil {
		return fmt.Errorf("download zip: %w", err)
	}
	defer os.Remove(zipPath)

	fmt.Println("[2/3] A extrair zip para", frontendDir)
	if err := os.RemoveAll(frontendDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("limpar pasta frontend: %w", err)
	}
	if err := unzip(zipPath, frontendDir); err != nil {
		return fmt.Errorf("unzip: %w", err)
	}

	fmt.Println("[3/3] A recriar container Docker", containerName)
	if err := recreateNginxContainer(); err != nil {
		return fmt.Errorf("recriar container: %w", err)
	}

	return nil
}

// downloadZip faz download do ficheiro e devolve o caminho temporário
func downloadZip(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download falhou: status %s", resp.Status)
	}

	tmpFile, err := os.CreateTemp("", "hyperhive-web-dist-*.zip")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return "", err
	}

	return tmpFile.Name(), nil
}

// unzip extrai o zip para dest, criando pastas conforme necessário
func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		if err := extractZipFile(f, dest); err != nil {
			return err
		}
	}
	return nil
}

func extractZipFile(f *zip.File, dest string) error {
	fpath := filepath.Join(dest, f.Name)

	// segurança básica: impedir path traversal
	if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
		return fmt.Errorf("entrada inválida no zip: %s", f.Name)
	}

	if f.FileInfo().IsDir() {
		return os.MkdirAll(fpath, 0o755)
	}

	if err := os.MkdirAll(filepath.Dir(fpath), 0o755); err != nil {
		return err
	}

	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, rc)
	return err
}

// recreateNginxContainer remove o container antigo (se existir) e cria um novo
func recreateNginxContainer() error {
	// tenta remover o container antigo (ignoramos erro)
	_ = exec.Command("docker", "rm", "-f", containerName).Run()

	// docker run -d --name hyperhive-frontend --restart unless-stopped -p 8080:80 -v /opt/hyperhive/frontend:/usr/share/nginx/html:ro nginx:alpine
	cmd := exec.Command(
		"docker", "run", "-d",
		"--name", containerName,
		"--restart", "unless-stopped",
		"-p", hostPort+":80",
		"-v", frontendDir+":/usr/share/nginx/html:ro",
		"nginx:alpine",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
