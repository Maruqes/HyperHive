package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"512SvMan/automations"
	"512SvMan/db"
	"512SvMan/nots"
	"512SvMan/websocket"

	"github.com/Maruqes/512SvMan/logger"
)

// AutomationService is the master facade for HH Automations: config and
// trigger state come from HH Support, scripts and runs live on the master.
type AutomationService struct{}

var (
	// ErrAutomationInvalid wraps input errors that map to 400.
	ErrAutomationInvalid = errors.New("invalid automation request")
	// ErrAutomationNotRunning means a kill targeted a finished run.
	ErrAutomationNotRunning = errors.New("run is not in progress")
	// automationStartMu orders the running check, the run row insert and the start.
	automationStartMu sync.Mutex
)

const automationDBTimeout = 10 * time.Second

type AutomationNode struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
}

type AutomationView struct {
	automations.Config
	RunningRunID *string           `json:"running_run_id"`
	LastRun      *db.AutomationRun `json:"last_run"`
}

type AutomationListView struct {
	Paused      bool             `json:"paused"`
	Node        AutomationNode   `json:"node"`
	Automations []AutomationView `json:"automations"`
}

type AutomationSettingsView struct {
	Paused bool `json:"paused"`
	db.AutomationSettings
}

// AutomationFire is the request HH Support sends when a condition fires.
type AutomationFire struct {
	AutomationID   string         `json:"automation_id"`
	AutomationName string         `json:"automation_name"`
	Trigger        string         `json:"trigger"`
	Condition      string         `json:"condition"`
	Snapshot       map[string]any `json:"snapshot"`
	TimeoutSeconds int            `json:"timeout_seconds"`
}

// AutomationNotification is a notification requested by HH Support.
type AutomationNotification struct {
	Event    string `json:"event"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Body     string `json:"body"`
}

func view(config automations.Config, latest map[string]db.AutomationRun) AutomationView {
	result := AutomationView{Config: config}
	if id := automations.RunningRunID(config.ID); id != "" {
		result.RunningRunID = &id
	}
	if run, ok := latest[config.ID]; ok {
		result.LastRun = &run
	}
	return result
}

func latestRuns(ctx context.Context) map[string]db.AutomationRun {
	latest, err := db.LatestAutomationRuns(ctx)
	if err != nil {
		logger.Errorf("load latest automation runs: %v", err)
		return map[string]db.AutomationRun{}
	}
	return latest
}

func (AutomationService) List(ctx context.Context) (AutomationListView, error) {
	list, err := automations.ListConfigs(ctx)
	if err != nil {
		return AutomationListView{}, err
	}
	available, version := automations.NodeStatus()
	result := AutomationListView{Paused: list.Paused, Node: AutomationNode{Available: available, Version: version}, Automations: []AutomationView{}}
	latest := latestRuns(ctx)
	for _, config := range list.Automations {
		result.Automations = append(result.Automations, view(config, latest))
	}
	return result, nil
}

func (AutomationService) Get(ctx context.Context, id string) (AutomationView, error) {
	config, err := automations.GetConfig(ctx, id)
	if err != nil {
		return AutomationView{}, err
	}
	return view(config, latestRuns(ctx)), nil
}

// Create stores the config in Support, then writes a starter main.ts. A failed
// starter write is only logged: the editor can create the file later.
func (AutomationService) Create(ctx context.Context, body json.RawMessage) (AutomationView, error) {
	var config automations.Config
	if err := automations.SupportRequest(ctx, http.MethodPost, "/automations", body, &config); err != nil {
		return AutomationView{}, err
	}
	if err := automations.CreateStarter(config.ID); err != nil {
		logger.Errorf("create starter script for automation %s: %v", config.ID, err)
	}
	return view(config, nil), nil
}

func (AutomationService) Update(ctx context.Context, id string, body json.RawMessage) (AutomationView, error) {
	if !automations.ValidID(id) {
		return AutomationView{}, &automations.SupportError{Status: http.StatusNotFound, Message: "not found"}
	}
	var config automations.Config
	if err := automations.SupportRequest(ctx, http.MethodPut, "/automations/"+url.PathEscape(id), body, &config); err != nil {
		return AutomationView{}, err
	}
	return view(config, latestRuns(ctx)), nil
}

// Delete kills the running run, removes the Support config, then the scripts
// and run history. If Support fails nothing else is removed; a later file or
// history failure leaves orphans that a retry cleans up.
func (AutomationService) Delete(ctx context.Context, id string) error {
	if !automations.ValidID(id) {
		return &automations.SupportError{Status: http.StatusNotFound, Message: "not found"}
	}
	if runID := automations.RunningRunID(id); runID != "" {
		if err := automations.Kill(runID); err != nil && !errors.Is(err, automations.ErrNotFound) {
			return fmt.Errorf("kill run %s: %w", runID, err)
		}
	}
	err := automations.SupportRequest(ctx, http.MethodDelete, "/automations/"+url.PathEscape(id), nil, nil)
	if err != nil && !automations.IsSupportNotFound(err) {
		return err
	}
	if err := automations.RemoveScripts(id); err != nil {
		return fmt.Errorf("remove automation scripts: %w", err)
	}
	if err := db.DeleteAutomationRuns(ctx, id); err != nil {
		return fmt.Errorf("delete automation runs: %w", err)
	}
	return nil
}

func (AutomationService) Evaluate(ctx context.Context, body json.RawMessage) (json.RawMessage, error) {
	var result json.RawMessage
	err := automations.SupportRequest(ctx, http.MethodPost, "/automations/evaluate", body, &result)
	return result, err
}

// AppValues returns the current HH Support values of one App (or instance),
// as a condition would read them now.
func (AutomationService) AppValues(ctx context.Context, app, instance string) (json.RawMessage, error) {
	query := url.Values{"app": {app}}
	if instance != "" {
		query.Set("instance", instance)
	}
	var result json.RawMessage
	err := automations.SupportRequest(ctx, http.MethodGet, "/automations/values?"+query.Encode(), nil, &result)
	return result, err
}

// ConditionCatalog returns the Apps, instances and streams a condition can read.
func (AutomationService) ConditionCatalog(ctx context.Context) (json.RawMessage, error) {
	var result json.RawMessage
	err := automations.SupportRequest(ctx, http.MethodGet, "/automations/catalog", nil, &result)
	return result, err
}

func (AutomationService) Settings(ctx context.Context) (AutomationSettingsView, error) {
	var support struct {
		Paused bool `json:"paused"`
	}
	if err := automations.SupportRequest(ctx, http.MethodGet, "/automations/settings", nil, &support); err != nil {
		return AutomationSettingsView{}, err
	}
	settings, err := db.GetAutomationSettings(ctx)
	if err != nil {
		return AutomationSettingsView{}, err
	}
	return AutomationSettingsView{Paused: support.Paused, AutomationSettings: settings}, nil
}

func (AutomationService) SaveSettings(ctx context.Context, settings AutomationSettingsView) (AutomationSettingsView, error) {
	if settings.RetentionDays < 1 || settings.RetentionDays > 3650 || settings.RetentionCount < 1 || settings.RetentionCount > 100000 {
		return AutomationSettingsView{}, fmt.Errorf("%w: retention_days must be 1-3650 and retention_count 1-100000", ErrAutomationInvalid)
	}
	if err := db.SaveAutomationSettings(ctx, settings.AutomationSettings); err != nil {
		return AutomationSettingsView{}, err
	}
	body := map[string]bool{"paused": settings.Paused}
	if err := automations.SupportRequest(ctx, http.MethodPut, "/automations/settings", body, nil); err != nil {
		return AutomationSettingsView{}, err
	}
	pruneAutomationRuns()
	return settings, nil
}

// Run starts a manual run with the automation's configured timeout.
func (AutomationService) Run(ctx context.Context, id string) (string, error) {
	config, err := automations.GetConfig(ctx, id)
	if err != nil {
		return "", err
	}
	return startAutomationRun(ctx, automations.RunSpec{AutomationID: config.ID, AutomationName: config.Name, Trigger: automations.TriggerManual, Condition: config.Condition, Snapshot: map[string]any{}, Timeout: time.Duration(config.Timeout) * time.Second})
}

// Fire starts a condition run requested by HH Support.
func (AutomationService) Fire(ctx context.Context, fire AutomationFire) (string, error) {
	if !automations.ValidID(fire.AutomationID) || fire.Trigger != automations.TriggerCondition || fire.TimeoutSeconds < 1 || fire.TimeoutSeconds > 86400 {
		return "", fmt.Errorf("%w: automation_id, trigger \"condition\" and timeout_seconds 1-86400 are required", ErrAutomationInvalid)
	}
	if fire.Snapshot == nil {
		fire.Snapshot = map[string]any{}
	}
	return startAutomationRun(ctx, automations.RunSpec{AutomationID: fire.AutomationID, AutomationName: fire.AutomationName, Trigger: fire.Trigger, Condition: fire.Condition, Snapshot: fire.Snapshot, Timeout: time.Duration(fire.TimeoutSeconds) * time.Second})
}

func (AutomationService) Install(ctx context.Context) (string, error) {
	return startAutomationRun(ctx, automations.RunSpec{AutomationName: "npm install", Trigger: automations.TriggerNPMInstall, Snapshot: map[string]any{}})
}

func (AutomationService) Kill(ctx context.Context, runID string) error {
	err := automations.Kill(runID)
	if !errors.Is(err, automations.ErrNotFound) {
		return err
	}
	if _, dbErr := db.GetAutomationRun(ctx, runID); dbErr == nil {
		return ErrAutomationNotRunning
	} else if !errors.Is(dbErr, sql.ErrNoRows) {
		return dbErr
	}
	return automations.ErrNotFound
}

func (AutomationService) Runs(ctx context.Context, automationID string, limit int) ([]db.AutomationRun, error) {
	if limit <= 0 {
		limit = 50
	}
	return db.ListAutomationRuns(ctx, automationID, min(limit, 200))
}

func (AutomationService) RunDetail(ctx context.Context, runID string) (db.AutomationRun, error) {
	return db.GetAutomationRun(ctx, runID)
}

func (AutomationService) Notify(ctx context.Context, notification AutomationNotification) error {
	severity, err := nots.ParseSeverity(notification.Severity)
	if err != nil || notification.Title == "" || len(notification.Title) > 300 || len(notification.Body) > 4000 {
		return fmt.Errorf("%w: valid severity and a title are required", ErrAutomationInvalid)
	}
	event := notification.Event
	if event == "" {
		event = "automation.support"
	}
	if len(event) > 100 {
		return fmt.Errorf("%w: event is too long", ErrAutomationInvalid)
	}
	// Keep Support events inside the automation category.
	event = nots.CategoryAutomation + "." + strings.TrimPrefix(event, nots.CategoryAutomation+".")
	notifyOperation(ctx, event, severity, "", notification.Title, notification.Body)
	return nil
}

func broadcastAutomationRun(runID string) {
	ctx, cancel := context.WithTimeout(context.Background(), automationDBTimeout)
	defer cancel()
	run, err := db.GetAutomationRun(ctx, runID)
	if err != nil {
		logger.Errorf("load automation run %s: %v", runID, err)
		return
	}
	run.Output = nil
	data, err := json.Marshal(run)
	if err != nil {
		return
	}
	websocket.BroadcastMessage(websocket.Message{Type: "AutomationRun", Data: string(data), Extra: runID})
}

// startAutomationRun records the run, then starts it. The record is written
// before the process exists so the finish callback always has a row to update.
func startAutomationRun(ctx context.Context, spec automations.RunSpec) (string, error) {
	automationStartMu.Lock()
	defer automationStartMu.Unlock()
	if spec.Trigger != automations.TriggerNPMInstall {
		if err := automations.EnsureWorkspace(); err != nil {
			return "", err
		}
		if automations.RunningRunID(spec.AutomationID) != "" {
			return "", automations.ErrAlreadyRunning
		}
		if _, err := automations.EntryFile(spec.AutomationID); err != nil {
			return "", fmt.Errorf("%w: %v", ErrAutomationInvalid, err)
		}
	} else if automations.InstallRunID() != "" {
		return "", automations.ErrAlreadyRunning
	}
	runID, err := automations.NewRunID()
	if err != nil {
		return "", err
	}
	spec.RunID = runID
	run := db.AutomationRun{ID: runID, AutomationID: spec.AutomationID, AutomationName: spec.AutomationName, Trigger: spec.Trigger, Condition: spec.Condition, Snapshot: spec.Snapshot, Status: automations.StatusRunning, StartedAt: time.Now().UTC()}
	if err := db.InsertAutomationRun(ctx, run); err != nil {
		return "", fmt.Errorf("record automation run: %w", err)
	}
	err = automations.Start(spec, func(pgid int) {
		ctx, cancel := context.WithTimeout(context.Background(), automationDBTimeout)
		defer cancel()
		if err := db.SetAutomationRunPGID(ctx, runID, pgid); err != nil {
			logger.Errorf("record automation run %s process group: %v", runID, err)
		}
	}, func(result automations.Result) { finishAutomationRun(spec, result) })
	if err != nil {
		finishAutomationRun(spec, automations.Result{Status: automations.StatusFailed, Error: err.Error(), EndedAt: time.Now().UTC()})
		return "", err
	}
	broadcastAutomationRun(runID)
	return runID, nil
}

func finishAutomationRun(spec automations.RunSpec, result automations.Result) {
	ctx, cancel := context.WithTimeout(context.Background(), automationDBTimeout)
	defer cancel()
	if err := db.FinishAutomationRun(ctx, spec.RunID, result.Status, result.ExitCode, result.EndedAt, result.Output, result.Truncated, result.Error); err != nil {
		logger.Errorf("finish automation run %s: %v", spec.RunID, err)
	}
	broadcastAutomationRun(spec.RunID)
	if result.Status == automations.StatusFailed || result.Status == automations.StatusTimeout {
		event, title := "automation.failed", "Automation "+result.Status+": "+spec.AutomationName
		if spec.Trigger == automations.TriggerNPMInstall {
			event, title = "automation.install_failed", "Automation npm install "+result.Status
		}
		body := result.Error
		if result.ExitCode != nil {
			body = fmt.Sprintf("Exit code %d. %s", *result.ExitCode, result.Error)
		}
		notifyOperation(ctx, event, nots.SeverityError, "", title, body)
	}
	pruneAutomationRuns()
}

func pruneAutomationRuns() {
	ctx, cancel := context.WithTimeout(context.Background(), automationDBTimeout)
	defer cancel()
	settings, err := db.GetAutomationSettings(ctx)
	if err == nil {
		err = db.PruneAutomationRuns(ctx, settings, time.Now().UTC())
	}
	if err != nil {
		logger.Errorf("prune automation runs: %v", err)
	}
}

// RecoverAutomationRuns runs at master startup: runs still marked running were
// cut off by a restart, so their leftover processes are killed and they are
// marked interrupted.
func RecoverAutomationRuns(ctx context.Context) {
	runs, err := db.RunningAutomationRuns(ctx)
	if err != nil {
		logger.Errorf("load interrupted automation runs: %v", err)
		return
	}
	for _, run := range runs {
		if killed := automations.KillOrphans(run.ID); killed > 0 {
			logger.Infof("killed %d leftover processes of automation run %s", killed, run.ID)
		}
		if err := db.FinishAutomationRun(ctx, run.ID, automations.StatusInterrupted, nil, time.Now().UTC(), "", false, "master restarted during the run"); err != nil {
			logger.Errorf("mark automation run %s interrupted: %v", run.ID, err)
		}
	}
	pruneAutomationRuns()
}
