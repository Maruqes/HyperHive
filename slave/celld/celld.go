package celld

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	pb "github.com/Maruqes/512SvMan/api/proto/celld"
	"google.golang.org/protobuf/proto"
)

const DataDir = "/var/lib/hyperhive/celld"
const Binary = "/usr/local/bin/celld"

// Local belongs to the slave agent, including the agent running on the master.
var Local *Manager

type Manager struct {
	op                      sync.Mutex
	mu                      sync.Mutex
	dir, binary, ip         string
	lock                    *os.File
	life                    context.Context
	stopLife                context.CancelFunc
	config                  *pb.Config
	cancel                  context.CancelFunc
	done                    chan struct{}
	running                 bool
	lastError               string
	closed                  bool
	restartDelay, stopGrace time.Duration
}

func New(dir, binary, ip string) (*Manager, error) {
	if parsed := net.ParseIP(ip); parsed == nil || (!parsed.IsPrivate() && !parsed.IsLoopback()) {
		return nil, fmt.Errorf("celld requires a private management IP")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "agent.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another agent owns celld: %w", err)
	}
	life, cancel := context.WithCancel(context.Background())
	return &Manager{dir: dir, binary: binary, ip: ip, lock: f, life: life, stopLife: cancel, restartDelay: 10 * time.Second, stopGrace: 60 * time.Second}, nil
}

func (m *Manager) Resume(ctx context.Context) error {
	if _, err := os.Stat(filepath.Join(m.dir, "enabled")); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(m.dir, "config.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var c pb.Config
	if err = json.Unmarshal(data, &c); err != nil {
		return err
	}
	// An agent restart must also leave one lease lifetime after the previous child.
	timer := time.NewTimer(m.restartDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.life.Done():
		return m.life.Err()
	case <-timer.C:
	}
	return m.Configure(ctx, &c, true)
}

var versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

func validate(c *pb.Config) error {
	if c == nil || !versionPattern.MatchString(c.Version) {
		return fmt.Errorf("an exact celld release is required")
	}
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("invalid storage endpoint")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || (!ip.IsPrivate() && !ip.IsLoopback()) || u.Port() == "" {
		return fmt.Errorf("storage must use the management network")
	}
	if c.Bucket != "s3://hyperhive-celld" || c.Region != "us-east-1" || c.AccessKey == "" || c.SecretKey == "" {
		return fmt.Errorf("incomplete fleet configuration")
	}
	return nil
}

// Only these documented transitions preserve the fleet protocol and disk format.
func rollingAllowed(from, to string) bool {
	if from == "" || from == to {
		return true
	}
	return (from == "v0.6.0" && to == "v0.6.1") ||
		(from == "v0.6.1" && to == "v0.6.2") || (from == "v0.6.2" && to == "v0.6.1")
}

func (m *Manager) Configure(ctx context.Context, c *pb.Config, start bool) error {
	ctx, cancelSetup := context.WithCancel(ctx)
	stopSetup := context.AfterFunc(m.life, cancelSetup)
	defer stopSetup()
	defer cancelSetup()
	m.op.Lock()
	defer m.op.Unlock()
	if m.closed || m.life.Err() != nil {
		return fmt.Errorf("celld manager is shutting down")
	}
	if err := validate(c); err != nil {
		return err
	}
	// Read disk as well: an agent restart must not bypass the upgrade guard.
	var previous pb.Config
	if data, err := os.ReadFile(filepath.Join(m.dir, "config.json")); err == nil {
		if err = json.Unmarshal(data, &previous); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !rollingAllowed(previous.Version, c.Version) {
		return fmt.Errorf("celld %s to %s needs a coordinated stopped-fleet upgrade", previous.Version, c.Version)
	}
	if previous.Bucket != "" && (previous.Bucket != c.Bucket || previous.Endpoint != c.Endpoint || previous.AccessKey != c.AccessKey || previous.SecretKey != c.SecretKey) {
		return fmt.Errorf("refusing to replace existing fleet storage or credentials")
	}
	m.mu.Lock()
	same := proto.Equal(m.config, c)
	m.mu.Unlock()
	if !same {
		m.stop()
	}
	if err := m.install(ctx, c.Version); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	path := filepath.Join(m.dir, "config.json")
	if err = os.WriteFile(path+".tmp", data, 0600); err != nil {
		return err
	}
	if err = os.Rename(path+".tmp", path); err != nil {
		return err
	}
	m.mu.Lock()
	m.config = proto.Clone(c).(*pb.Config)
	m.mu.Unlock()
	if start && m.cancel == nil {
		if err := os.WriteFile(filepath.Join(m.dir, "enabled"), []byte("enabled\n"), 0600); err != nil {
			return err
		}
		// The child lifetime is deliberately independent of the Configure RPC.
		life, cancel := context.WithCancel(m.life)
		m.cancel, m.done = cancel, make(chan struct{})
		go m.watch(life, proto.Clone(c).(*pb.Config), m.done)
	}
	return nil
}

func (m *Manager) install(ctx context.Context, version string) error {
	out, err := exec.CommandContext(ctx, m.binary, "--version").Output()
	if err == nil {
		fields := strings.Fields(string(out))
		if len(fields) >= 2 && "v"+strings.TrimPrefix(fields[1], "v") == version {
			return nil
		}
	}
	var missing []string
	for _, dependency := range []string{"curl", "gzip"} {
		if _, err := exec.LookPath(dependency); err != nil {
			missing = append(missing, dependency)
		}
	}
	if len(missing) > 0 {
		cmd := exec.CommandContext(ctx, "dnf", append([]string{"install", "-y", "ca-certificates"}, missing...)...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("install celld download dependencies: %w", err)
		}
	}
	// Download separately so a failed curl cannot become a successful empty shell.
	script, err := exec.CommandContext(ctx, "curl", "-fsSL", "--max-time", "60", "https://celld.dev/install.sh").Output()
	if err != nil {
		return fmt.Errorf("download celld installer: %w", err)
	}
	cmd := exec.CommandContext(ctx, "sh")
	cmd.Stdin = strings.NewReader(string(script))
	cmd.Env = append(os.Environ(), "CELLD_VERSION="+version, "CELLD_INSTALL_ROOT="+filepath.Dir(filepath.Dir(m.binary)))
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("install celld: %w", err)
	}
	out, err = exec.CommandContext(ctx, m.binary, "--version").Output()
	fields := strings.Fields(string(out))
	if err != nil || len(fields) < 2 || "v"+strings.TrimPrefix(fields[1], "v") != version {
		return fmt.Errorf("installed celld does not report %s", version)
	}
	return nil
}

func (m *Manager) watch(ctx context.Context, c *pb.Config, done chan struct{}) {
	// Linux ties Pdeathsig to the creating thread; keep that thread alive with the child.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(done)
	for ctx.Err() == nil {
		cmd := exec.Command(m.binary, "--bucket", c.Bucket, "--endpoint", c.Endpoint, "--region", c.Region,
			"--listen", net.JoinHostPort(m.ip, "8080"), "--internal-listen", net.JoinHostPort(m.ip, "8081"), "--advertise", net.JoinHostPort(m.ip, "8081"))
		cmd.Env = append(os.Environ(), "AWS_ACCESS_KEY_ID="+c.AccessKey, "AWS_SECRET_ACCESS_KEY="+c.SecretKey, "AWS_REGION="+c.Region,
			"CELLD_WATCH="+filepath.Join(m.dir, "data"), "CELLD_TTL_MS=10000", "AWS_SESSION_TOKEN=")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		// The inherited lock covers the gap between an agent crash and child death.
		cmd.ExtraFiles = []*os.File{m.lock}
		cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
		err := cmd.Start()
		if err == nil {
			m.mu.Lock()
			m.running, m.lastError = true, ""
			m.mu.Unlock()
			wait := make(chan error, 1)
			go func() { wait <- cmd.Wait() }()
			select {
			case err = <-wait:
			case <-ctx.Done():
				_ = cmd.Process.Signal(syscall.SIGTERM)
				timer := time.NewTimer(m.stopGrace)
				select {
				case err = <-wait:
				case <-timer.C:
					_ = cmd.Process.Kill()
					err = <-wait
				}
				timer.Stop()
			}
		}
		m.mu.Lock()
		m.running = false
		if ctx.Err() == nil {
			m.lastError = fmt.Sprintf("celld exited (%v); retrying in %s", err, m.restartDelay)
			log.Print(m.lastError)
		}
		m.mu.Unlock()
		timer := time.NewTimer(m.restartDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (m *Manager) stop() {
	if m.cancel != nil {
		m.cancel()
		<-m.done
		m.cancel = nil
	}
}

func (m *Manager) Close() {
	m.stopLife()
	m.op.Lock()
	defer m.op.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	m.stop()
	_ = m.lock.Close()
}

func (m *Manager) Status() *pb.Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := &pb.Status{NodeIp: m.ip, Running: m.running, Error: m.lastError}
	if m.config != nil {
		s.Version = m.config.Version
	}
	return s
}
