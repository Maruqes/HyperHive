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
	CUR_LINK      = "https://github.com/JotaBarbosaDev/HyperHive/releases/download/v1.0.1/web-dist.zip"
	frontendDir   = "/opt/hyperhive/frontend"
	containerName = "hyperhive-frontend"
	hostPort      = "8079" // http://servidor:8080

	nginxConfPath = "/opt/hyperhive/nginx.conf"
)

func setupFrontendContainer() error {
	fmt.Println("[1/4] A garantir nginx.conf de SPA...")
	if err := ensureNginxConfig(); err != nil {
		return fmt.Errorf("criar nginx.conf: %w", err)
	}

	fmt.Println("[2/4] A fazer download do web-dist.zip...")
	zipPath, err := downloadZip(CUR_LINK)
	if err != nil {
		return fmt.Errorf("download zip: %w", err)
	}
	defer os.Remove(zipPath)

	fmt.Println("[3/4] A extrair zip para", frontendDir)
	if err := os.RemoveAll(frontendDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("limpar pasta frontend: %w", err)
	}
	if err := unzip(zipPath, frontendDir); err != nil {
		return fmt.Errorf("unzip: %w", err)
	}

	fmt.Println("[4/4] A recriar container Docker", containerName)
	if err := recreateNginxContainer(); err != nil {
		return fmt.Errorf("recriar container: %w", err)
	}

	return nil
}

// cria / atualiza o nginx.conf com fallback SPA
func ensureNginxConfig() error {
	if err := os.MkdirAll(filepath.Dir(nginxConfPath), 0o755); err != nil {
		return err
	}

	const nginxConf = `
server {
    listen 80;
    server_name _;

    root /usr/share/nginx/html;
    index index.html;

    location / {
        try_files $uri $uri/ /index.html;
    }

    location ~* \.(js|css|png|jpg|jpeg|gif|svg|ico|woff2?)$ {
        try_files $uri =404;
        access_log off;
        add_header Cache-Control "public, max-age=31536000, immutable";
    }
}
`
	return os.WriteFile(nginxConfPath, []byte(strings.TrimSpace(nginxConf)+"\n"), 0o644)
}

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

func recreateNginxContainer() error {
	// remove container antigo se existir
	_ = exec.Command("docker", "rm", "-f", containerName).Run()

	// docker run -d --name hyperhive-frontend --restart unless-stopped \
	//   -p 8080:80 \
	//   -v /opt/hyperhive/frontend:/usr/share/nginx/html:ro \
	//   -v /opt/hyperhive/nginx.conf:/etc/nginx/conf.d/default.conf:ro \
	//   nginx:alpine
	cmd := exec.Command(
		"docker", "run", "-d",
		"--name", containerName,
		"--restart", "unless-stopped",
		"-p", hostPort+":80",
		"-v", frontendDir+":/usr/share/nginx/html:ro",
		"-v", nginxConfPath+":/etc/nginx/conf.d/default.conf:ro",
		"nginx:alpine",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
