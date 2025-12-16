package main

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
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
	// helpers (self-contained)
	pickExtractRoot := func(dir string) (string, error) {
		ents, err := os.ReadDir(dir)
		if err != nil {
			return "", err
		}
		// se tiver exactamente 1 dir e mais nada, usa essa como root
		if len(ents) == 1 && ents[0].IsDir() {
			return filepath.Join(dir, ents[0].Name()), nil
		}
		return dir, nil
	}

	dirNotEmpty := func(dir string) (bool, error) {
		f, err := os.Open(dir)
		if err != nil {
			return false, err
		}
		defer f.Close()
		_, err = f.Readdirnames(1)
		if err == io.EOF {
			return false, nil
		}
		return err == nil, err
	}

	copyDir := func(src, dst string) error {
		return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(src, path)
			if err != nil {
				return err
			}
			target := filepath.Join(dst, rel)

			info, err := d.Info()
			if err != nil {
				return err
			}

			if d.IsDir() {
				return os.MkdirAll(target, info.Mode())
			}

			// symlinks: replica o link
			if info.Mode()&os.ModeSymlink != 0 {
				link, err := os.Readlink(path)
				if err != nil {
					return err
				}
				_ = os.RemoveAll(target)
				return os.Symlink(link, target)
			}

			// ficheiro normal
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			defer in.Close()

			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
			if err != nil {
				return err
			}
			defer func() {
				_ = out.Close()
			}()

			if _, err := io.Copy(out, in); err != nil {
				return err
			}
			return out.Close()
		})
	}

	rollback := func(oldBackup string, hadOld bool) {
		_ = os.RemoveAll(frontendDir)
		if hadOld {
			_ = os.Rename(oldBackup, frontendDir)
		}
	}

	// 0) garantir que o pai do destino existe (/opt/hyperhive)
	if err := os.MkdirAll(filepath.Dir(frontendDir), 0o755); err != nil {
		return fmt.Errorf("create frontend parent dir: %w", err)
	}

	// 1) workspace temporário (podes manter /tmp; o EXDEV fica coberto)
	tempWorkDir := "/tmp/hyperhive"
	fmt.Println("[1/5] Clearing and preparing temporary workspace at", tempWorkDir)
	if err := os.RemoveAll(tempWorkDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear tempWorkDir: %w", err)
	}
	if err := os.MkdirAll(tempWorkDir, 0o755); err != nil {
		return fmt.Errorf("create tempWorkDir: %w", err)
	}
	defer os.RemoveAll(tempWorkDir)

	// 2) download
	fmt.Println("[2/5] Downloading web-dist.zip...")
	zipPath, err := downloadZip(CUR_LINK)
	if err != nil {
		return fmt.Errorf("download zip: %w", err)
	}
	defer os.Remove(zipPath)

	// 3) extract
	extractDir := filepath.Join(tempWorkDir, "extract")
	fmt.Println("[3/5] Extracting zip to", extractDir)
	if err := os.RemoveAll(extractDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear extractDir: %w", err)
	}
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return fmt.Errorf("create extractDir: %w", err)
	}
	if err := unzip(zipPath, extractDir); err != nil {
		return fmt.Errorf("unzip: %w", err)
	}

	srcDir, err := pickExtractRoot(extractDir)
	if err != nil {
		return fmt.Errorf("pickExtractRoot: %w", err)
	}
	ok, err := dirNotEmpty(srcDir)
	if err != nil {
		return fmt.Errorf("check extracted content: %w", err)
	}
	if !ok {
		return fmt.Errorf("extracted directory is empty: %s", srcDir)
	}

	// 4) swap atómico com backup (não apaga o antigo antes de ter novo pronto)
	oldBackup := frontendDir + ".old"
	fmt.Println("[4/5] Swapping frontend directory into place:", frontendDir)
	if err := os.RemoveAll(oldBackup); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove old backup: %w", err)
	}

	hadOld := false
	if _, err := os.Stat(frontendDir); err == nil {
		if err := os.Rename(frontendDir, oldBackup); err != nil {
			return fmt.Errorf("backup old frontend: %w", err)
		}
		hadOld = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat frontendDir: %w", err)
	}

	// tentar rename; se EXDEV, copia
	if err := os.Rename(srcDir, frontendDir); err != nil {
		if linkErr, ok := err.(*os.LinkError); ok && linkErr.Err == syscall.EXDEV {
			fmt.Println("Detected cross-device move; copying files instead.")
			if err := copyDir(srcDir, frontendDir); err != nil {
				rollback(oldBackup, hadOld)
				return fmt.Errorf("copy frontend to destination: %w", err)
			}
		} else {
			rollback(oldBackup, hadOld)
			return fmt.Errorf("move frontend to destination: %w", err)
		}
	}

	// 5) config + container (se falhar, faz rollback para o backup)
	fmt.Println("[5/5] Ensuring nginx.conf and recreating container...")
	if err := ensureNginxConfig(); err != nil {
		rollback(oldBackup, hadOld)
		return fmt.Errorf("create nginx.conf: %w", err)
	}
	if err := recreateNginxContainer(); err != nil {
		rollback(oldBackup, hadOld)
		return fmt.Errorf("recreate container: %w", err)
	}

	// sucesso: limpa backup
	if hadOld {
		_ = os.RemoveAll(oldBackup)
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
