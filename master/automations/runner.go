package automations

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"512SvMan/websocket"
)

const (
	// MaxOutput caps the stored and streamed output of one run.
	MaxOutput = 1 << 20
	// installTimeout bounds npm install, including installing Node itself.
	installTimeout  = 30 * time.Minute
	nodeSetupLimit  = 10 * time.Minute
	logFlushPeriod  = 250 * time.Millisecond
	runIDEnv        = "HH_AUTOMATION_RUN_ID"
	installRunnerID = "__npm-install__"

	StatusRunning     = "running"
	StatusSucceeded   = "succeeded"
	StatusFailed      = "failed"
	StatusTimeout     = "timeout"
	StatusKilled      = "killed"
	StatusInterrupted = "interrupted"

	TriggerManual     = "manual"
	TriggerCondition  = "condition"
	TriggerNPMInstall = "npm-install"
)

var ErrAlreadyRunning = errors.New("automation is already running")

// RunSpec describes one run. AutomationID is empty for npm install.
type RunSpec struct {
	RunID          string
	AutomationID   string
	AutomationName string
	Trigger        string
	Condition      string
	Snapshot       map[string]any
	Timeout        time.Duration
}

// Result is the outcome of a finished run.
type Result struct {
	Status    string
	ExitCode  *int
	Output    string
	Truncated bool
	Error     string
	EndedAt   time.Time
}

type process struct {
	spec    RunSpec
	key     string
	cancel  context.CancelFunc
	mu      sync.Mutex
	pgid    int
	killed  bool
	timeout bool
}

var (
	runnerMu  sync.Mutex
	processes = map[string]*process{} // by run ID
	byKey     = map[string]string{}   // automation ID (or install key) -> run ID
	tokens    = map[string]string{}   // run token -> run ID
)

// NewRunID returns a random run identifier.
func NewRunID() (string, error) { return randomToken(16) }

func randomToken(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

// ValidRunToken reports whether token belongs to a run in progress. Scripts use
// it as their API credential; it is revoked when the run ends.
func ValidRunToken(token string) bool {
	if token == "" {
		return false
	}
	runnerMu.Lock()
	defer runnerMu.Unlock()
	for candidate := range tokens {
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(token)) == 1 {
			return true
		}
	}
	return false
}

// RunningRunID returns the run in progress for an automation, or "".
func RunningRunID(automationID string) string {
	runnerMu.Lock()
	defer runnerMu.Unlock()
	return byKey[automationID]
}

// InstallRunID returns the npm install in progress, or "".
func InstallRunID() string { return RunningRunID(installRunnerID) }

// Start reserves the automation and runs it in the background. onStarted gets
// the process group ID; onFinish always runs exactly once.
func Start(spec RunSpec, onStarted func(pgid int), onFinish func(Result)) error {
	key := spec.AutomationID
	if spec.Trigger == TriggerNPMInstall {
		key = installRunnerID
	} else if !ValidID(key) {
		return ErrInvalidPath
	}
	ctx, cancel := context.WithCancel(context.Background())
	proc := &process{spec: spec, key: key, cancel: cancel}
	runnerMu.Lock()
	if byKey[key] != "" {
		runnerMu.Unlock()
		cancel()
		return ErrAlreadyRunning
	}
	byKey[key] = spec.RunID
	processes[spec.RunID] = proc
	runnerMu.Unlock()
	go proc.run(ctx, onStarted, onFinish)
	return nil
}

// Kill stops a run's whole process group. It returns ErrNotFound when the run
// is not in progress on this master.
func Kill(runID string) error {
	runnerMu.Lock()
	proc := processes[runID]
	runnerMu.Unlock()
	if proc == nil {
		return ErrNotFound
	}
	proc.mu.Lock()
	proc.killed = true
	pgid := proc.pgid
	proc.mu.Unlock()
	proc.cancel()
	if pgid > 0 {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
	return nil
}

func (proc *process) run(ctx context.Context, onStarted func(int), onFinish func(Result)) {
	output := newOutput(proc.spec.RunID)
	result := Result{}
	token := ""
	defer func() {
		output.close()
		runnerMu.Lock()
		delete(processes, proc.spec.RunID)
		delete(byKey, proc.key)
		delete(tokens, token)
		runnerMu.Unlock()
		proc.cancel()
		result.Output, result.Truncated = output.text()
		result.EndedAt = time.Now().UTC()
		onFinish(result)
	}()
	timeout := proc.spec.Timeout
	if proc.spec.Trigger == TriggerNPMInstall {
		timeout = installTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := ensureNode(ctx, output); err != nil {
		result.Status, result.Error = proc.failure(ctx), err.Error()
		return
	}
	var err error
	if token, err = randomToken(32); err != nil {
		result.Status, result.Error = StatusFailed, "create run token: "+err.Error()
		return
	}
	cmd, err := proc.command(token)
	if err != nil {
		result.Status, result.Error = StatusFailed, err.Error()
		return
	}
	cmd.Stdout, cmd.Stderr = output, output
	runnerMu.Lock()
	tokens[token] = proc.spec.RunID
	runnerMu.Unlock()

	// Pdeathsig is tied to the OS thread that forks, so keep this goroutine on
	// one thread until the child exits; the child then dies with the master.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := cmd.Start(); err != nil {
		result.Status, result.Error = StatusFailed, "start: "+err.Error()
		return
	}
	pgid := cmd.Process.Pid
	proc.mu.Lock()
	proc.pgid = pgid
	killed := proc.killed
	proc.mu.Unlock()
	if killed {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
	if onStarted != nil {
		onStarted(pgid)
	}
	stop := context.AfterFunc(ctx, func() {
		proc.mu.Lock()
		if !proc.killed && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			proc.timeout = true
		}
		proc.mu.Unlock()
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	})
	waitErr := cmd.Wait()
	stop()
	// Kill leftovers that kept the group alive after the leader exited.
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	code := cmd.ProcessState.ExitCode()
	if code >= 0 {
		result.ExitCode = &code
	}
	proc.mu.Lock()
	defer proc.mu.Unlock()
	switch {
	case proc.killed:
		result.Status = StatusKilled
	case proc.timeout:
		result.Status, result.Error = StatusTimeout, fmt.Sprintf("timed out after %s", timeout)
	case waitErr == nil:
		result.Status = StatusSucceeded
	default:
		result.Status = StatusFailed
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			result.Error = waitErr.Error()
		}
	}
}

func (proc *process) failure(ctx context.Context) string {
	proc.mu.Lock()
	defer proc.mu.Unlock()
	if proc.killed {
		return StatusKilled
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return StatusTimeout
	}
	return StatusFailed
}

func baseEnv() []string {
	return []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/root",
		"LANG=C.UTF-8",
	}
}

func (proc *process) command(token string) (*exec.Cmd, error) {
	env := append(baseEnv(), runIDEnv+"="+proc.spec.RunID)
	var cmd *exec.Cmd
	if proc.spec.Trigger == TriggerNPMInstall {
		cmd = exec.Command("npm", "install", "--no-audit", "--no-fund")
		cmd.Dir = Dir()
	} else {
		entry, err := EntryFile(proc.spec.AutomationID)
		if err != nil {
			return nil, err
		}
		runContext, err := json.Marshal(map[string]any{
			"runId": proc.spec.RunID, "automationId": proc.spec.AutomationID, "automationName": proc.spec.AutomationName,
			"trigger": proc.spec.Trigger, "condition": proc.spec.Condition, "snapshot": proc.spec.Snapshot,
		})
		if err != nil {
			return nil, err
		}
		cmd = exec.Command("node", "--experimental-strip-types", "--disable-warning=ExperimentalWarning",
			filepath.Join(runtimeDir(), "runtime.mjs"), entry)
		cmd.Dir = ScriptDir(proc.spec.AutomationID)
		env = append(env, "HH_API_URL="+masterURL(), "HH_TOKEN="+token, "HH_CONTEXT="+string(runContext))
	}
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	return cmd, nil
}

var (
	nodeMu      sync.Mutex
	nodeVersion string
	nodeChecked time.Time
)

// NodeStatus reports whether node is installed and its version (cached 1 min).
func NodeStatus() (bool, string) {
	nodeMu.Lock()
	defer nodeMu.Unlock()
	if time.Since(nodeChecked) > time.Minute {
		nodeChecked = time.Now()
		nodeVersion = ""
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "node", "--version").Output(); err == nil {
			nodeVersion = strings.TrimSpace(string(out))
		}
	}
	return nodeVersion != "", nodeVersion
}

// ensureNode installs nodejs and npm with dnf when either is missing.
func ensureNode(ctx context.Context, output io.Writer) error {
	_, nodeErr := exec.LookPath("node")
	_, npmErr := exec.LookPath("npm")
	if nodeErr == nil && npmErr == nil {
		return nil
	}
	fmt.Fprintln(output, "[hyperhive] Node.js or npm is missing; installing nodejs and npm with dnf")
	setup, cancel := context.WithTimeout(ctx, nodeSetupLimit)
	defer cancel()
	cmd := exec.CommandContext(setup, "dnf", "install", "-y", "nodejs", "npm")
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install nodejs and npm: %w", err)
	}
	nodeMu.Lock()
	nodeChecked = time.Time{}
	nodeMu.Unlock()
	return nil
}

// KillOrphans kills processes left by a run of a previous master process. It
// matches the run ID in each process environment, so unrelated processes that
// reused a PID are never touched.
func KillOrphans(runID string) int {
	marker := []byte(runIDEnv + "=" + runID + "\x00")
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	killed := 0
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		environ, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "environ"))
		if err != nil || !bytes.Contains(append(environ, 0), marker) {
			continue
		}
		if syscall.Kill(pid, syscall.SIGKILL) == nil {
			killed++
		}
	}
	return killed
}

// output collects a run's output up to MaxOutput and streams it in batches.
type output struct {
	runID     string
	mu        sync.Mutex
	buffer    bytes.Buffer
	pending   bytes.Buffer
	truncated bool
	done      chan struct{}
	stopped   sync.WaitGroup
}

func newOutput(runID string) *output {
	out := &output{runID: runID, done: make(chan struct{})}
	out.stopped.Add(1)
	go out.flushLoop()
	return out
}

func (out *output) Write(data []byte) (int, error) {
	out.mu.Lock()
	defer out.mu.Unlock()
	if out.truncated {
		return len(data), nil
	}
	room := MaxOutput - out.buffer.Len()
	chunk := data
	if len(chunk) > room {
		chunk = chunk[:room]
		out.truncated = true
	}
	out.buffer.Write(chunk)
	out.pending.Write(chunk)
	if out.truncated {
		notice := "\n[hyperhive] output truncated at 1 MiB\n"
		out.pending.WriteString(notice)
	}
	return len(data), nil
}

func (out *output) flush() {
	out.mu.Lock()
	chunk := out.pending.String()
	out.pending.Reset()
	out.mu.Unlock()
	if chunk != "" {
		websocket.BroadcastMessage(websocket.Message{Type: "AutomationLog", Data: chunk, Extra: out.runID})
	}
}

func (out *output) flushLoop() {
	defer out.stopped.Done()
	ticker := time.NewTicker(logFlushPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			out.flush()
		case <-out.done:
			out.flush()
			return
		}
	}
}

func (out *output) close() {
	close(out.done)
	out.stopped.Wait()
}

func (out *output) text() (string, bool) {
	out.mu.Lock()
	defer out.mu.Unlock()
	text := out.buffer.String()
	if out.truncated {
		text += "\n[hyperhive] output truncated at 1 MiB\n"
	}
	return text, out.truncated
}
