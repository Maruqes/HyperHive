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
	"syscall"
)

const (
	frontendDir   = "/opt/hyperhive/frontend"
	tempWorkDir   = "/tmp/hyperhive"
	containerName = "hyperhive-frontend"
	hostPort      = "8079" // http://servidor:8080

	nginxConfPath = "/opt/hyperhive/nginx.conf"
)

var CUR_LINK = "https://github.com/JotaBarbosaDev/HyperHive/releases/latest/download/web-dist.zip"

func setupFrontendContainer() error {
	// Step 1: prepare temporary workspace and download+extract there
	fmt.Println("[1/4] Clearing and preparing temporary workspace at", tempWorkDir)
	if err := os.RemoveAll(tempWorkDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear tempWorkDir: %w", err)
	}
	if err := os.MkdirAll(tempWorkDir, 0o755); err != nil {
		return fmt.Errorf("create tempWorkDir: %w", err)
	}

	fmt.Println("[2/4] Downloading web-dist.zip...")
	zipPath, err := downloadZip(CUR_LINK)
	if err != nil {
		return fmt.Errorf("download zip: %w", err)
	}
	// remove downloaded zip when finished
	defer os.Remove(zipPath)
	// cleanup temporary workspace when finished
	defer os.RemoveAll(tempWorkDir)

	extractDir := filepath.Join(tempWorkDir, "extract")
	fmt.Println("[3/4] Extracting zip to temporary workspace", extractDir)
	if err := os.RemoveAll(extractDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear extractDir: %w", err)
	}
	if err := unzip(zipPath, extractDir); err != nil {
		return fmt.Errorf("unzip: %w", err)
	}

	// Check if extraction created a nested subdirectory and unwrap it
	entries, err := os.ReadDir(extractDir)
	if err != nil {
		return fmt.Errorf("read extractDir: %w", err)
	}
	if len(entries) == 1 && entries[0].IsDir() {
		nestedDir := filepath.Join(extractDir, entries[0].Name())
		tempDir := filepath.Join(tempWorkDir, "temp-extract")
		if err := os.Rename(nestedDir, tempDir); err != nil {
			return fmt.Errorf("unwrap nested directory: %w", err)
		}
		if err := os.RemoveAll(extractDir); err != nil {
			return fmt.Errorf("remove extractDir: %w", err)
		}
		extractDir = tempDir
	}

	// Atomically replace the published frontend directory
	// Remove old frontend dir only after successful extraction
	if err := os.RemoveAll(frontendDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear frontend directory: %w", err)
	}

	// Try to rename the extracted dir into place. If the rename fails
	// with EXDEV (invalid cross-device link), perform a recursive copy
	// instead and then clean up the temp extract dir.
	if err := os.Rename(extractDir, frontendDir); err != nil {
		if linkErr, ok := err.(*os.LinkError); ok && linkErr.Err == syscall.EXDEV {
			fmt.Println("Detected cross-device move; copying files instead.")
			if err := copyDir(extractDir, frontendDir); err != nil {
				return fmt.Errorf("copy frontend to destination: %w", err)
			}
			if err := os.RemoveAll(extractDir); err != nil {
				return fmt.Errorf("cleanup extractDir: %w", err)
			}
		} else {
			return fmt.Errorf("move frontend to destination: %w", err)
		}
	}

	// Ensure nginx config exists (persisted at nginxConfPath)
	fmt.Println("[4/4] Ensuring nginx.conf for SPA...")
	if err := ensureNginxConfig(); err != nil {
		return fmt.Errorf("create nginx.conf: %w", err)
	}

	// Recreate container after files and config are ready
	if err := recreateNginxContainer(); err != nil {
		return fmt.Errorf("recreate container: %w", err)
	}

	return nil
}

// create/update nginx.conf with SPA fallback
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
		return "", fmt.Errorf("download failed: status %s", resp.Status)
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
		return fmt.Errorf("invalid zip entry: %s", f.Name)
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

// copyDir recursively copies src -> dst preserving file modes and symlinks.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		// handle symlinks
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return os.Symlink(linkTarget, target)
		}

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

func recreateNginxContainer() error {
	// remove old container if exists
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
