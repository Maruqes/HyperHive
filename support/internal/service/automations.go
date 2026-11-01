package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"sort"
	"strings"
	"time"

	"hyperhive/support/internal/domain"
	"hyperhive/support/internal/master"
	"hyperhive/support/internal/security"
	"hyperhive/support/internal/storage/sqlite"
)

// Master starts runs and sends notifications on the HyperHive master.
type Master interface {
	Fire(context.Context, master.FireRequest) (string, error)
	Notify(context.Context, master.Notification) error
}

// SetMaster configures where condition fires go. Without it fires record an error.
func (service *Service) SetMaster(client Master) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.master = client
}

func (service *Service) loadAutomations() error {
	automations, err := service.store.Automations()
	if err != nil {
		return err
	}
	for index := range automations {
		automation := automations[index]
		service.automations[automation.ID] = &automation
	}
	if service.automationsPaused, err = service.store.AutomationsPaused(); err != nil {
		return err
	}
	values, err := service.store.LatestValues()
	if err != nil {
		return err
	}
	for _, value := range values {
		service.recordLatest(value)
	}
	return nil
}

func (service *Service) recordLatest(value sqlite.LatestValue) {
	participants := service.latest[value.AppID]
	if participants == nil {
		participants = map[string]map[string]sqlite.LatestValue{}
		service.latest[value.AppID] = participants
	}
	streams := participants[value.Instance]
	if streams == nil {
		streams = map[string]sqlite.LatestValue{}
		participants[value.Instance] = streams
	}
	if previous, exists := streams[value.Stream]; !exists || previous.SampleID < value.SampleID {
		streams[value.Stream] = value
	}
}

func copyAutomation(automation *domain.Automation) domain.Automation {
	copied := *automation
	copied.State.RecentFires = slices.Clone(automation.State.RecentFires)
	if copied.State.Snapshot == nil {
		copied.State.Snapshot = map[string]any{}
	}
	if copied.State.RecentFires == nil {
		copied.State.RecentFires = []time.Time{}
	}
	return copied
}

func (service *Service) Automations() (domain.AutomationList, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	list := domain.AutomationList{Paused: service.automationsPaused, Automations: []domain.Automation{}}
	for _, automation := range service.automations {
		list.Automations = append(list.Automations, copyAutomation(automation))
	}
	sort.Slice(list.Automations, func(left, right int) bool {
		a, b := list.Automations[left], list.Automations[right]
		if !strings.EqualFold(a.Name, b.Name) {
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
		return a.ID < b.ID
	})
	return list, nil
}

func (service *Service) Automation(id string) (domain.Automation, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	automation, exists := service.automations[id]
	if !exists {
		return domain.Automation{}, domain.NotFound()
	}
	return copyAutomation(automation), nil
}

func (service *Service) CreateAutomation(input domain.AutomationInput) (domain.Automation, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	automation := domain.Automation{}
	if err := domain.ApplyAutomationInput(&automation, input, true); err != nil {
		return domain.Automation{}, err
	}
	id, err := security.NewToken()
	if err != nil {
		return domain.Automation{}, err
	}
	now := service.now()
	automation.ID, automation.CreatedAt, automation.UpdatedAt = id, now, now
	automation.State = domain.AutomationState{Armed: true, Baseline: true}
	if err := service.store.SaveAutomation(automation); err != nil {
		return domain.Automation{}, err
	}
	service.automations[id] = &automation
	service.evaluateAutomation(&automation, service.conditionData(now), now)
	return copyAutomation(&automation), nil
}

func (service *Service) UpdateAutomation(id string, input domain.AutomationInput) (domain.Automation, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	existing, exists := service.automations[id]
	if !exists {
		return domain.Automation{}, domain.NotFound()
	}
	updated := copyAutomation(existing)
	if err := domain.ApplyAutomationInput(&updated, input, false); err != nil {
		return domain.Automation{}, err
	}
	if updated.Enabled && !existing.Enabled {
		updated.State.DisabledReason = ""
		updated.State.RecentFires = []time.Time{}
		updated.State.Baseline = true
	}
	if updated.Condition != existing.Condition {
		updated.State.Baseline = true
		updated.State.TrueSince = nil
		delete(service.automationDeps, id)
	}
	now := service.now()
	updated.UpdatedAt = now
	if err := service.store.SaveAutomation(updated); err != nil {
		return domain.Automation{}, err
	}
	*existing = updated
	service.evaluateAutomation(existing, service.conditionData(now), now)
	return copyAutomation(existing), nil
}

func (service *Service) DeleteAutomation(id string) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if err := service.store.DeleteAutomation(id); err != nil {
		return err
	}
	delete(service.automations, id)
	delete(service.automationDeps, id)
	return nil
}

func (service *Service) AutomationSettings() domain.AutomationSettings {
	service.mu.Lock()
	defer service.mu.Unlock()
	return domain.AutomationSettings{Paused: service.automationsPaused}
}

func (service *Service) SaveAutomationSettings(settings domain.AutomationSettings) (domain.AutomationSettings, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if err := service.store.SetAutomationsPaused(settings.Paused); err != nil {
		return domain.AutomationSettings{}, err
	}
	service.automationsPaused = settings.Paused
	return settings, nil
}

// TestCondition evaluates a condition against current data without firing.
func (service *Service) TestCondition(condition string) (domain.ConditionResult, error) {
	condition = strings.TrimSpace(condition)
	if !domain.ValidLength(condition, 1, 4000) {
		return domain.ConditionResult{}, domain.Invalid("condition is required (maximum 4000 characters)")
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	now := service.now()
	result, _ := domain.EvaluateCondition(condition, service.conditionData(now))
	return result, nil
}

// ConditionCatalog lists every App with its instances, streams and current
// values so the condition editor can offer completions.
func (service *Service) ConditionCatalog() (domain.ConditionCatalog, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	now := service.now()
	apps, err := service.store.Apps(now)
	if err != nil {
		return domain.ConditionCatalog{}, err
	}
	data := service.conditionData(now)
	catalog := domain.ConditionCatalog{Apps: []domain.CatalogApp{}}
	for _, app := range apps {
		declared, err := service.store.Streams(app.ID)
		if err != nil {
			return domain.ConditionCatalog{}, err
		}
		view, err := data.AppView(app.ID, "")
		if err != nil {
			return domain.ConditionCatalog{}, err
		}
		entry := domain.CatalogApp{ID: app.ID, Name: app.Name, Connectivity: app.Connectivity, Health: app.Health, Instances: []string{}, Streams: []domain.CatalogStream{}}
		for _, instance := range app.Instances {
			entry.Instances = append(entry.Instances, instance.ID)
		}
		for _, stream := range declared {
			entry.Streams = append(entry.Streams, domain.CatalogStream{Name: stream.Name, Type: stream.Type, Unit: stream.Unit, Label: stream.Label, Value: view.Streams[stream.Name]})
		}
		catalog.Apps = append(catalog.Apps, entry)
	}
	sort.SliceStable(catalog.Apps, func(i, j int) bool {
		return strings.ToLower(catalog.Apps[i].Name) < strings.ToLower(catalog.Apps[j].Name)
	})
	return catalog, nil
}

// evaluateAutomations re-evaluates every automation, or with appID only those
// that read that App (or whose App references are still unresolved). The
// caller holds service.mu.
func (service *Service) evaluateAutomations(appID string) {
	if len(service.automations) == 0 {
		return
	}
	now := service.now()
	data := service.conditionData(now)
	ids := make([]string, 0, len(service.automations))
	for id := range service.automations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if deps := service.automationDeps[id]; appID != "" && len(deps) > 0 && !deps[appID] {
			continue
		}
		service.evaluateAutomation(service.automations[id], data, now)
	}
}

func persistedState(state domain.AutomationState) string {
	state.Snapshot = nil
	state.LastEvaluatedAt = nil
	return domain.EncodeJSON(state)
}

func (service *Service) evaluateAutomation(automation *domain.Automation, data domain.ConditionData, now time.Time) {
	state := &automation.State
	before := persistedState(*state)
	defer func() {
		if persistedState(*state) != before {
			if err := service.store.SaveAutomationState(automation.ID, *state); err != nil {
				log.Printf("save automation %s state: %v", automation.ID, err)
			}
		}
	}()
	if automation.Condition == "" {
		state.ConditionValue, state.LastError, state.Snapshot, state.TrueSince = nil, "", nil, nil
		state.Armed, state.Baseline = true, true
		return
	}
	result, deps := domain.EvaluateCondition(automation.Condition, data)
	service.automationDeps[automation.ID] = deps
	evaluated := now
	state.ConditionValue, state.LastError, state.Snapshot, state.LastEvaluatedAt = result.Value, result.Error, result.Snapshot, &evaluated
	value := *result.Value
	if !value {
		state.TrueSince = nil
		state.Armed = true
	} else if state.TrueSince == nil {
		since := now
		state.TrueSince = &since
	}
	switch {
	case !automation.Enabled || service.automationsPaused:
		state.Baseline = true
	case state.Baseline:
		state.Baseline = false
		state.Armed = !value
	case value && state.Armed && now.Sub(*state.TrueSince) >= time.Duration(automation.For)*time.Second &&
		(state.LastFiredAt == nil || now.Sub(*state.LastFiredAt) >= time.Duration(automation.Cooldown)*time.Second):
		service.fire(automation, now)
	}
}

// fire consumes the edge and dispatches the run, or auto-disables the
// automation when the hourly cap is reached. The caller holds service.mu.
func (service *Service) fire(automation *domain.Automation, now time.Time) {
	state := &automation.State
	recent := []time.Time{}
	for _, fired := range state.RecentFires {
		if now.Sub(fired) < time.Hour {
			recent = append(recent, fired)
		}
	}
	state.RecentFires = recent
	if len(recent) >= automation.MaxRunsPerHour {
		automation.Enabled = false
		automation.UpdatedAt = now
		state.Baseline = true
		state.DisabledReason = fmt.Sprintf("Auto-disabled at %s: reached %d condition runs in one hour", now.Format(time.RFC3339), automation.MaxRunsPerHour)
		if err := service.store.SaveAutomation(*automation); err != nil {
			log.Printf("auto-disable automation %s: %v", automation.ID, err)
		}
		notification := master.Notification{Event: "automation.disabled", Severity: "warning", Title: "Automation disabled: " + automation.Name, Body: state.DisabledReason}
		service.dispatch(func(ctx context.Context, client Master) error {
			return client.Notify(ctx, notification)
		}, nil)
		return
	}
	fired := now
	state.Armed = false
	state.LastFiredAt = &fired
	state.LastFireError = ""
	state.RecentFires = append(state.RecentFires, fired)
	snapshot, _ := json.Marshal(state.Snapshot)
	request := master.FireRequest{AutomationID: automation.ID, AutomationName: automation.Name, Trigger: "condition", Condition: automation.Condition, TimeoutSeconds: automation.Timeout}
	_ = json.Unmarshal(snapshot, &request.Snapshot)
	id := automation.ID
	service.dispatch(func(ctx context.Context, client Master) error {
		_, err := client.Fire(ctx, request)
		return err
	}, func(err error) {
		if automation, exists := service.automations[id]; exists && err != nil {
			automation.State.LastFireError = err.Error()
			if err := service.store.SaveAutomationState(id, automation.State); err != nil {
				log.Printf("save automation %s state: %v", id, err)
			}
		}
	})
}

// dispatch calls the master outside service.mu; done runs under service.mu.
func (service *Service) dispatch(call func(context.Context, Master) error, done func(error)) {
	client := service.master
	service.dispatches.Add(1)
	go func() {
		defer service.dispatches.Done()
		err := errors.New("master is not configured")
		if client != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			err = call(ctx, client)
			cancel()
		}
		if err != nil && !errors.Is(err, master.ErrAlreadyRunning) {
			log.Printf("automation master call failed: %v", err)
		}
		if done != nil {
			service.mu.Lock()
			done(err)
			service.mu.Unlock()
		}
	}()
}

type conditionData struct {
	service *Service
	now     time.Time
	views   map[string]*domain.AppView
	errors  map[string]error
}

func (service *Service) conditionData(now time.Time) *conditionData {
	return &conditionData{service: service, now: now, views: map[string]*domain.AppView{}, errors: map[string]error{}}
}

func (data *conditionData) AppView(name, instance string) (*domain.AppView, error) {
	key := strings.ToLower(name) + "\x00" + instance
	if view, cached := data.views[key]; cached {
		return view, nil
	}
	if err, cached := data.errors[key]; cached {
		return nil, err
	}
	view, err := data.load(name, instance)
	if err != nil {
		data.errors[key] = err
		return nil, err
	}
	data.views[key] = view
	return view, nil
}

func (data *conditionData) load(name, instance string) (*domain.AppView, error) {
	store := data.service.store
	id, err := store.AppIDByName(name)
	var problem *domain.Error
	if errors.As(err, &problem) && problem.Kind == domain.MissingResource {
		return nil, fmt.Errorf("unknown App %q", name)
	} else if err != nil {
		return nil, err
	}
	app, err := store.App(id, data.now)
	if err != nil {
		return nil, err
	}
	latest := data.service.latest[id]
	view := &domain.AppView{ID: id, Label: app.Name, Streams: map[string]any{}}
	online := map[string]bool{}
	if instance == "" {
		view.Fields = map[string]any{"health": app.Health, "connectivity": app.Connectivity, "health_message": app.HealthMessage}
		direct, err := store.DirectParticipant(id)
		if err != nil {
			return nil, err
		}
		if direct != nil {
			domain.ParticipantState(direct, app.Timeout, data.now)
			online[""] = direct.Connectivity == "online"
		}
		for _, participant := range app.Instances {
			online[participant.ID] = participant.Connectivity == "online"
		}
	} else {
		view.Label = app.Name + "[" + instance + "]"
		index := slices.IndexFunc(app.Instances, func(item domain.Instance) bool { return item.ID == instance })
		if index < 0 {
			return nil, fmt.Errorf("App %q has no instance %q", app.Name, instance)
		}
		participant := app.Instances[index]
		view.Fields = map[string]any{"health": participant.Health, "connectivity": participant.Connectivity, "health_message": participant.HealthMessage}
		online[instance] = participant.Connectivity == "online"
	}
	newest := map[string]sqlite.LatestValue{}
	for participant, isOnline := range online {
		if !isOnline {
			continue
		}
		for stream, value := range latest[participant] {
			if previous, exists := newest[stream]; !exists || previous.SampleID < value.SampleID {
				newest[stream] = value
			}
		}
	}
	for stream, value := range newest {
		var decoded any
		if err := json.Unmarshal(value.Value, &decoded); err == nil {
			view.Streams[stream] = decoded
		}
	}
	return view, nil
}
