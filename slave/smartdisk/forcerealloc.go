package smartdisk

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ForceReallocMode representa o tipo de execução badblocks.
type ForceReallocMode int

const (
	// ForceReallocModeDestructive usa "badblocks -wsv" (DESTRÓI todos os dados).
	ForceReallocModeDestructive ForceReallocMode = iota
	// ForceReallocModeNonDestructive usa "badblocks -nsv" (tenta manter os dados).
	ForceReallocModeNonDestructive
)

// ForceReallocStatus guarda o estado atual de uma execução badblocks.
type ForceReallocStatus struct {
	Device           string           `json:"device"`
	Mode             ForceReallocMode `json:"mode"`
	StartedAt        time.Time        `json:"started_at"`
	Elapsed          time.Duration    `json:"elapsed"`
	Percent          float64          `json:"percent"`
	Pattern          string           `json:"pattern"`
	ReadErrors       int              `json:"read_errors"`
	WriteErrors      int              `json:"write_errors"`
	CorruptionErrors int              `json:"corruption_errors"`
	LastLine         string           `json:"last_line"`
	Completed        bool             `json:"completed"`
	Err              error            `json:"-"`
}

// representação interna do job
type forceReallocJob struct {
	mu     sync.RWMutex
	status ForceReallocStatus

	cmd    *exec.Cmd
	ctx    context.Context
	cancel context.CancelFunc
}

// regex para parse ao output do badblocks
var (
	// Ex: " 12.34% done"
	percentRe = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)%\s+done`)
	// Ex: "Testing with pattern 0xaa:" ou "Testing with pattern random pattern:"
	patternRe = regexp.MustCompile(`Testing with pattern\s+(.+?)(?::|$)`)
	// Ex: "(0/0/0 errors)"
	errorsRe = regexp.MustCompile(`\(([0-9]+)/([0-9]+)/([0-9]+)\s+errors\)`)
)

// chamado com job.mu já locked
func (j *forceReallocJob) updateFromLine(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	j.status.LastLine = line

	if m := percentRe.FindStringSubmatch(line); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			j.status.Percent = v
		}
	}
	if m := patternRe.FindStringSubmatch(line); m != nil {
		j.status.Pattern = strings.TrimSpace(m[1])
	}
	if m := errorsRe.FindStringSubmatch(line); m != nil {
		if v, err := strconv.Atoi(m[1]); err == nil {
			j.status.ReadErrors = v
		}
		if v, err := strconv.Atoi(m[2]); err == nil {
			j.status.WriteErrors = v
		}
		if v, err := strconv.Atoi(m[3]); err == nil {
			j.status.CorruptionErrors = v
		}
	}
}

// snapshot devolve uma cópia do estado atual.
func (j *forceReallocJob) snapshot() ForceReallocStatus {
	j.mu.RLock()
	defer j.mu.RUnlock()

	st := j.status
	if !st.StartedAt.IsZero() && !st.Completed {
		st.Elapsed = time.Since(st.StartedAt)
	}
	return st
}

// lineBuffer junta bytes de stdout/stderr e separa em "linhas".
type lineBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	job *forceReallocJob
}

func (lb *lineBuffer) Write(p []byte) (int, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	for i, b := range p {
		switch b {
		case '\r', '\n':
			if lb.buf.Len() == 0 {
				continue
			}
			line := lb.buf.String()
			lb.buf.Reset()
			lb.job.mu.Lock()
			lb.job.updateFromLine(line)
			lb.job.mu.Unlock()
		default:
			if err := lb.buf.WriteByte(b); err != nil {
				return i, err
			}
		}
	}
	return len(p), nil
}

// flush processa qualquer conteúdo restante no buffer como linha final
func (lb *lineBuffer) flush() {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if lb.buf.Len() > 0 {
		line := lb.buf.String()
		lb.buf.Reset()
		lb.job.mu.Lock()
		lb.job.updateFromLine(line)
		lb.job.mu.Unlock()
	}
}

// ForceReallocManager gere todos os badblocks em curso.
type ForceReallocManager struct {
	mu   sync.RWMutex
	jobs map[string]*forceReallocJob
}

// NewForceReallocManager cria um novo manager.
func NewForceReallocManager() *ForceReallocManager {
	m := &ForceReallocManager{
		jobs: make(map[string]*forceReallocJob),
	}
	// limpar jobs completados periodicamente para evitar memory leak
	go m.periodicCleanup()
	return m
}

var defaultForceReallocManager = NewForceReallocManager()

// periodicCleanup remove jobs completados há mais de 1 hora
func (m *ForceReallocManager) periodicCleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.Lock()
		for device, job := range m.jobs {
			job.mu.RLock()
			if job.status.Completed && time.Since(job.status.StartedAt.Add(job.status.Elapsed)) > 7*24*time.Hour {
				delete(m.jobs, device)
			}
			job.mu.RUnlock()
		}
		m.mu.Unlock()
	}
}

// StartFullWipe inicia um badblocks DESTRUTIVO ("badblocks -wsv <dev>").
// TODOS os dados no disco serão apagados.
func StartFullWipe(device string) (string, error) {
	_, err := defaultForceReallocManager.start(device, ForceReallocModeDestructive)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Limpeza destrutiva iniciada em %s (badblocks -wsv).", device), nil
}

// StartNonDestructiveRealloc inicia um badblocks NÃO destrutivo ("badblocks -nsv <dev>")
// tentando forçar realocação sem apagar o conteúdo.
func StartNonDestructiveRealloc(device string) (string, error) {
	_, err := defaultForceReallocManager.start(device, ForceReallocModeNonDestructive)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Reallocação não destrutiva iniciada em %s (badblocks -nsv).", device), nil
}

// GetReallocStatus devolve o estado para um device.
func GetReallocStatus(device string) (ForceReallocStatus, error) {
	return defaultForceReallocManager.Status(device)
}

// ListReallocStatus devolve o estado de todos os devices com jobs conhecidos.
func ListReallocStatus() []ForceReallocStatus {
	return defaultForceReallocManager.AllStatuses()
}

// CancelRealloc cancela um badblocks em execução para o device.
func CancelRealloc(device string) error {
	return defaultForceReallocManager.Cancel(device)
}

// start inicia o badblocks para um device e modo específico.
func (m *ForceReallocManager) start(device string, mode ForceReallocMode) (*forceReallocJob, error) {
	var err error
	if device, err = validateDevicePath(device); err != nil {
		return nil, err
	}

	m.mu.Lock()
	if existing, ok := m.jobs[device]; ok {
		// só permitimos 1 job ativo por disco
		existing.mu.RLock()
		alreadyRunning := !existing.status.Completed
		existing.mu.RUnlock()
		if alreadyRunning {
			m.mu.Unlock()
			return nil, fmt.Errorf("já existe uma limpeza/reallocação a correr em %s", device)
		}
		// remove job completado para libertar memória
		delete(m.jobs, device)
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &forceReallocJob{}
	job.ctx = ctx
	job.cancel = cancel
	job.status = ForceReallocStatus{
		Device:    device,
		Mode:      mode,
		StartedAt: time.Now(),
	}
	lineBuf := &lineBuffer{job: job}

	args := []string{}
	switch mode {
	case ForceReallocModeDestructive:
		// -w: write-mode (destrutivo), -s: progresso, -v: verbose
		args = []string{"-wsv", device}
	case ForceReallocModeNonDestructive:
		// -n: non-destructive read-write, -s: progresso, -v: verbose
		args = []string{"-nsv", device}
	default:
		m.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("modo de reallocação inválido: %d", mode)
	}

	cmd := exec.CommandContext(ctx, "badblocks", args...)
	cmd.Stdout = lineBuf
	cmd.Stderr = lineBuf
	job.cmd = cmd

	m.jobs[device] = job
	m.mu.Unlock()

	if err := cmd.Start(); err != nil {
		m.mu.Lock()
		delete(m.jobs, device)
		m.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("falha ao iniciar badblocks em %s: %w", device, err)
	}

	// esperar em background
	go func() {
		err := cmd.Wait()
		// processar qualquer linha incompleta restante
		lineBuf.flush()
		job.mu.Lock()
		job.status.Completed = true
		job.status.Err = err
		job.status.Elapsed = time.Since(job.status.StartedAt)
		job.mu.Unlock()
		// limpar contexto
		cancel()
	}()

	return job, nil
}

// Status devolve o estado atual para um device.
func (m *ForceReallocManager) Status(device string) (ForceReallocStatus, error) {
	var err error
	if device, err = validateDevicePath(device); err != nil {
		return ForceReallocStatus{}, err
	}
	m.mu.RLock()
	job, ok := m.jobs[device]
	m.mu.RUnlock()
	if !ok {
		return ForceReallocStatus{}, fmt.Errorf("nenhuma limpeza/reallocação conhecida para %s", device)
	}
	return job.snapshot(), nil
}

// AllStatuses devolve snapshot de todos os jobs.
func (m *ForceReallocManager) AllStatuses() []ForceReallocStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]ForceReallocStatus, 0, len(m.jobs))
	for _, job := range m.jobs {
		out = append(out, job.snapshot())
	}
	return out
}

// Cancel cancela o job em execução para um device, se existir.
func (m *ForceReallocManager) Cancel(device string) error {
	var err error
	if device, err = validateDevicePath(device); err != nil {
		return err
	}
	m.mu.RLock()
	job, ok := m.jobs[device]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("nenhuma limpeza/reallocação a correr em %s", device)
	}

	job.mu.RLock()
	isCompleted := job.status.Completed
	job.mu.RUnlock()
	if isCompleted {
		return fmt.Errorf("a limpeza/reallocação em %s já terminou", device)
	}

	job.cancel()
	return nil
}
