package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// streamVMArtifact writes a fixed reviewed download endpoint into a new private
// server-side artifact beside its recovery record. It never accepts a URL or an
// output path from MCP tool arguments and never buffers a disk into JSON.
func (c *Client) streamVMArtifact(ctx context.Context, path, dir string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Hour)
	defer cancel()
	if !filepath.IsAbs(dir) {
		return nil, errors.New("VM artifact directory unavailable")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("VM artifact directory must be private")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+path, nil)
	if err != nil {
		return nil, errors.New("could not construct VM download")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/octet-stream")
	// Disk transfers can exceed ordinary JSON timeouts. Cancellation and any MCP
	// caller deadline still apply throughout the HTTP read and local write.
	transport := *c.http
	transport.Timeout = 0
	resp, err := transport.Do(req)
	if err != nil {
		return nil, errors.New("VM download failed or was canceled")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("VM download API returned HTTP " + strconv.Itoa(resp.StatusCode))
	}
	if strings.Split(resp.Header.Get("Content-Type"), ";")[0] != "application/octet-stream" {
		return nil, errors.New("VM download API returned an unexpected content type")
	}
	file, err := os.CreateTemp(dir, "disk-*.qcow2")
	if err != nil {
		return nil, errors.New("could not create private VM artifact")
	}
	complete := false
	defer func() {
		file.Close()
		if !complete {
			os.Remove(file.Name())
		}
	}()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), resp.Body)
	if err != nil || ctx.Err() != nil {
		return nil, errors.New("VM download interrupted; no complete artifact produced")
	}
	if size <= 0 || (resp.ContentLength >= 0 && size != resp.ContentLength) {
		return nil, errors.New("VM download length mismatch")
	}
	if file.Sync() != nil || file.Close() != nil {
		return nil, errors.New("could not persist VM artifact")
	}
	directory, err := os.Open(dir)
	if err != nil {
		return nil, errors.New("could not persist VM artifact directory")
	}
	syncErr := directory.Sync()
	directory.Close()
	if syncErr != nil {
		return nil, errors.New("could not persist VM artifact directory")
	}
	if ctx.Err() != nil {
		return nil, errors.New("VM download canceled before publication")
	}
	complete = true
	return json.Marshal(struct {
		ArtifactPath string `json:"artifact_path"`
		Bytes        int64  `json:"bytes"`
		SHA256       string `json:"sha256"`
	}{file.Name(), size, hex.EncodeToString(hash.Sum(nil))})
}
func (c *Client) ExportVM(ctx context.Context, in VMNameInput, dir string) (json.RawMessage, error) {
	if err := ValidateVMName(in.VMName); err != nil {
		return nil, err
	}
	return c.streamVMArtifact(ctx, "/virsh/export/"+url.PathEscape(in.VMName), dir)
}
func (c *Client) DownloadVMBackup(ctx context.Context, in VMIDInput, dir string) (json.RawMessage, error) {
	if err := ValidateVMID(in.ID); err != nil {
		return nil, err
	}
	return c.streamVMArtifact(ctx, "/virsh/downloadbackup/"+strconv.FormatInt(in.ID, 10), dir)
}
