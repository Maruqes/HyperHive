package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	pb "github.com/Maruqes/512SvMan/api/proto/celld"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type celldDiagnostic struct {
	Verdict string `json:"verdict"`
	Check   string `json:"check"`
	Detail  string `json:"detail"`
}

func diagnoseCelld(ctx context.Context, cfg *pb.Config) (map[string]celldDiagnostic, error) {
	cmd := exec.CommandContext(ctx, celldBinary, "diagnose", "--read-only", "--json", "--bucket", cfg.Bucket, "--endpoint", cfg.Endpoint, "--region", cfg.Region,
		"--listen", "127.0.0.1:0", "--internal-listen", "127.0.0.1:0")
	cmd.Env = append(os.Environ(), "AWS_ACCESS_KEY_ID="+cfg.AccessKey, "AWS_SECRET_ACCESS_KEY="+cfg.SecretKey, "AWS_REGION="+cfg.Region, "AWS_SESSION_TOKEN=")
	out, runErr := cmd.Output()
	peers, parseErr := parseCelldPeers(strings.NewReader(string(out)))
	return peers, errors.Join(runErr, parseErr)
}

func parseCelldPeers(reader io.Reader) (map[string]celldDiagnostic, error) {
	peers := make(map[string]celldDiagnostic)
	d := json.NewDecoder(reader)
	for {
		var row celldDiagnostic
		if err := d.Decode(&row); err != nil {
			if errors.Is(err, io.EOF) {
				return peers, nil
			}
			return peers, err
		}
		if strings.HasPrefix(row.Check, "peer ") {
			_, addr, ok := strings.Cut(row.Check, " at ")
			if ok {
				// A restarted node can share an address with its previous, unexpired lease.
				if prior, exists := peers[addr]; !exists || prior.Verdict != "ok" || row.Verdict == "ok" {
					peers[addr] = row
				}
			}
		}
	}
}

var celldHTTP = &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func celldReady(ctx context.Context, ip string, port uint32) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+net.JoinHostPort(ip, strconv.Itoa(int(port)))+"/.well-known/celld/health", nil)
	if err != nil {
		return err
	}
	resp, err := celldHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("readiness returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func waitCelldReady(ctx context.Context, cfg *pb.Config, ip string) error {
	wait, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	for {
		if err := celldReady(wait, ip, cfg.HttpPort); err == nil {
			probe, stop := context.WithTimeout(wait, 20*time.Second)
			peers, _ := diagnoseCelld(probe, cfg)
			stop()
			settled := true
			for _, peer := range peers {
				if peer.Verdict == "ok" && !strings.Contains(" "+peer.Detail+" ", " restoring=0 ") {
					settled = false
				}
			}
			if peer, ok := peers[net.JoinHostPort(ip, strconv.Itoa(int(cfg.InternalPort)))]; ok && peer.Verdict == "ok" && settled {
				return nil
			}
		}
		timer := time.NewTimer(3 * time.Second)
		select {
		case <-wait.Done():
			timer.Stop()
			return wait.Err()
		case <-timer.C:
		}
	}
}
