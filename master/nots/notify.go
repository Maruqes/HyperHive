package nots

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"512SvMan/db"

	"github.com/Maruqes/512SvMan/logger"
)

// Severity orders notifications from informational to critical.
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarning
	SeverityError
	SeverityCritical
)

func (s Severity) String() string {
	switch s {
	case SeverityWarning:
		return "warning"
	case SeverityError:
		return "error"
	case SeverityCritical:
		return "critical"
	default:
		return "info"
	}
}

// androidName matches the severity names of the Android notification channels.
func (s Severity) androidName() string {
	if s == SeverityWarning {
		return "warn"
	}
	return s.String()
}

func ParseSeverity(value string) (Severity, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "info":
		return SeverityInfo, nil
	case "warning", "warn":
		return SeverityWarning, nil
	case "error":
		return SeverityError, nil
	case "critical":
		return SeverityCritical, nil
	}
	return SeverityInfo, fmt.Errorf("invalid severity %q (use info, warning, error or critical)", value)
}

// Notification is one event sent to history and delivery channels.
type Notification struct {
	// Event is a stable key; the part before the first dot is the category used by rules.
	Event    string
	Severity Severity
	Title    string
	Body     string
	URL      string
	Machine  string
	// DedupKey suppresses repeats with the same key inside the category's dedup window.
	DedupKey string
}

// Category is the rule group of the event, e.g. "health" for "health.smart".
func (n Notification) Category() string {
	category, _, _ := strings.Cut(n.Event, ".")
	if category == "" {
		return CategoryLegacy
	}
	return category
}

// Notification categories. Rules are stored per category.
const (
	CategoryConnection = "connection"
	CategoryHealth     = "health"
	CategoryMetrics    = "metrics"
	CategoryOperation  = "operation"
	CategoryVM         = "vm"
	CategorySystem     = "system"
	CategoryLegacy     = "legacy"
)

// CategoryInfo describes a category for settings screens.
type CategoryInfo struct {
	Name        string
	Description string
}

var Categories = []CategoryInfo{
	{CategoryConnection, "Slave connection lost"},
	{CategoryHealth, "Disk and filesystem health: SMART, btrfs device errors, failed mounts"},
	{CategoryMetrics, "Host CPU temperature, memory, disk usage and disk temperature alerts"},
	{CategoryOperation, "Long operations: backups, migrations, btrfs operations, ISO downloads, VM creation"},
	{CategoryVM, "VM consistency problems such as duplicate VMs"},
	{CategorySystem, "Test notifications"},
	{CategoryLegacy, "Notifications from slaves that predate severities"},
}

func IsCategory(name string) bool {
	for _, c := range Categories {
		if c.Name == name {
			return true
		}
	}
	return false
}

// Rule is the delivery policy for one category.
type Rule struct {
	Category        string
	Enabled         bool
	MinPushSeverity Severity
	QuietStart      string // "HH:MM", master local time
	QuietEnd        string
}

// DefaultRule is used when a category has no stored rule.
// Finished operations push at low priority; everything else pushes from error up.
func DefaultRule(category string) Rule {
	rule := Rule{Category: category, Enabled: true, MinPushSeverity: SeverityError}
	if category == CategoryOperation {
		rule.MinPushSeverity = SeverityInfo
	}
	return rule
}

// Validate checks the rule's category and quiet-hours format.
func (r Rule) Validate() error {
	if !IsCategory(r.Category) {
		return fmt.Errorf("unknown notification category %q", r.Category)
	}
	if (r.QuietStart == "") != (r.QuietEnd == "") {
		return errors.New("quiet hours need both start and end, or neither")
	}
	for _, v := range []string{r.QuietStart, r.QuietEnd} {
		if v == "" {
			continue
		}
		if _, err := time.Parse("15:04", v); err != nil {
			return fmt.Errorf("invalid quiet hours time %q (use HH:MM)", v)
		}
	}
	return nil
}

// inQuietHours reports whether now falls inside the rule's quiet window; windows may cross midnight.
func (r Rule) inQuietHours(now time.Time) bool {
	if r.QuietStart == "" || r.QuietEnd == "" {
		return false
	}
	start, err1 := time.Parse("15:04", r.QuietStart)
	end, err2 := time.Parse("15:04", r.QuietEnd)
	if err1 != nil || err2 != nil {
		return false
	}
	minute := now.Hour()*60 + now.Minute()
	from := start.Hour()*60 + start.Minute()
	to := end.Hour()*60 + end.Minute()
	if from == to {
		return false
	}
	if from < to {
		return minute >= from && minute < to
	}
	return minute >= from || minute < to
}

// delivery is what one notification gets after the policy check.
type delivery struct {
	websocket bool
	push      bool
}

// decide applies a rule. History is always written; a disabled category only keeps history.
// Quiet hours hold back pushes below critical.
func decide(rule Rule, n Notification, now time.Time) delivery {
	if !rule.Enabled {
		return delivery{}
	}
	push := n.Severity >= rule.MinPushSeverity
	if push && n.Severity < SeverityCritical && rule.inQuietHours(now) {
		push = false
	}
	return delivery{websocket: true, push: push}
}

// dedupWindow is how long repeats of the same DedupKey are suppressed.
func dedupWindow(category string) time.Duration {
	switch category {
	case CategoryHealth, CategoryVM:
		return 24 * time.Hour
	default:
		return 6 * time.Hour
	}
}

// Channel delivers a notification to one kind of receiver.
type Channel interface {
	Name() string
	Send(ctx context.Context, n Notification) error
}

// HistoryStore persists notifications.
type HistoryStore interface {
	Save(ctx context.Context, n Notification, at time.Time) error
}

// RuleStore loads the rule for a category; ok is false when no rule is stored.
type RuleStore interface {
	Rule(ctx context.Context, category string) (rule Rule, ok bool, err error)
}

// Notifier runs policy, dedup, history and channel delivery.
type Notifier struct {
	history     HistoryStore
	rules       RuleStore
	websocket   Channel
	push        []Channel
	now         func() time.Time
	sendTimeout time.Duration

	mu        sync.Mutex
	lastSent  map[string]time.Time
	lastPrune time.Time

	wg sync.WaitGroup
}

func NewNotifier(history HistoryStore, rules RuleStore, websocket Channel, push ...Channel) *Notifier {
	return &Notifier{
		history:     history,
		rules:       rules,
		websocket:   websocket,
		push:        push,
		now:         time.Now,
		sendTimeout: 20 * time.Second,
		lastSent:    map[string]time.Time{},
	}
}

// Notify saves the notification and starts channel delivery in the background.
// It returns once history is written; each channel send has its own timeout.
func (x *Notifier) Notify(ctx context.Context, n Notification) error {
	n.Title = strings.TrimSpace(n.Title)
	n.Body = strings.TrimSpace(n.Body)
	if n.Title == "" {
		n.Title = "HyperHive"
	}
	if n.URL == "" {
		n.URL = "/"
	}
	if n.Severity < SeverityInfo || n.Severity > SeverityCritical {
		n.Severity = SeverityInfo
	}
	now := x.now()
	category := n.Category()

	if n.DedupKey != "" && !x.allow(n.DedupKey, dedupWindow(category), now) {
		logger.Infof("notification %s deduplicated (key %s)", n.Event, n.DedupKey)
		return nil
	}

	rule := DefaultRule(category)
	if x.rules != nil {
		stored, ok, err := x.rules.Rule(ctx, category)
		if err != nil {
			logger.Errorf("load notification rule %s, using defaults: %v", category, err)
		} else if ok {
			rule = stored
		}
	}
	d := decide(rule, n, now)

	var saveErr error
	if x.history != nil {
		if err := x.history.Save(ctx, n, now); err != nil {
			saveErr = fmt.Errorf("save notification %q: %w", n.Title, err)
			logger.Error(saveErr.Error())
		}
	}
	if d.websocket && x.websocket != nil {
		x.send(ctx, x.websocket, n)
	}
	if d.push {
		for _, ch := range x.push {
			x.send(ctx, ch, n)
		}
	}
	return saveErr
}

// Wait blocks until background channel sends finish. Used by tests and shutdown.
func (x *Notifier) Wait() { x.wg.Wait() }

func (x *Notifier) send(ctx context.Context, ch Channel, n Notification) {
	x.wg.Add(1)
	go func() {
		defer x.wg.Done()
		sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), x.sendTimeout)
		defer cancel()
		if err := ch.Send(sendCtx, n); err != nil {
			logger.Errorf("notification channel %s: %v", ch.Name(), err)
		}
	}()
}

// allow records key and reports whether it was not sent within window.
func (x *Notifier) allow(key string, window time.Duration, now time.Time) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	if now.Sub(x.lastPrune) > time.Hour {
		for k, at := range x.lastSent {
			if now.Sub(at) > 24*time.Hour {
				delete(x.lastSent, k)
			}
		}
		x.lastPrune = now
	}
	if last, ok := x.lastSent[key]; ok && now.Sub(last) < window {
		return false
	}
	x.lastSent[key] = now
	return true
}

// dbHistory stores notifications in the master SQLite history.
type dbHistory struct{}

func (dbHistory) Save(ctx context.Context, n Notification, at time.Time) error {
	return db.DbSaveNot(ctx, db.Not{
		Title:     n.Title,
		Body:      n.Body,
		RelURL:    n.URL,
		Critical:  n.Severity >= SeverityError,
		Severity:  n.Severity.String(),
		Event:     n.Event,
		Machine:   n.Machine,
		CreatedAt: at,
	})
}

// dbRules reads rules from the notification_rules table.
type dbRules struct{}

func (dbRules) Rule(ctx context.Context, category string) (Rule, bool, error) {
	stored, ok, err := db.DbGetNotificationRule(ctx, category)
	if err != nil || !ok {
		return Rule{}, ok, err
	}
	rule, err := ruleFromDB(stored)
	return rule, err == nil, err
}

func ruleFromDB(stored db.NotificationRule) (Rule, error) {
	severity, err := ParseSeverity(stored.MinPushSeverity)
	if err != nil {
		return Rule{}, fmt.Errorf("rule %s: %w", stored.Category, err)
	}
	return Rule{
		Category:        stored.Category,
		Enabled:         stored.Enabled,
		MinPushSeverity: severity,
		QuietStart:      stored.QuietStart,
		QuietEnd:        stored.QuietEnd,
	}, nil
}

var defaultNotifier = NewNotifier(dbHistory{}, dbRules{}, WebsocketChannel{}, WebPushChannel{}, FCMChannel{})

// Notify sends n through the process-wide notifier.
func Notify(ctx context.Context, n Notification) error {
	return defaultNotifier.Notify(ctx, n)
}

// GetRules returns the effective rule of every category.
func GetRules(ctx context.Context) ([]Rule, error) {
	rules := make([]Rule, 0, len(Categories))
	for _, c := range Categories {
		rule, ok, err := dbRules{}.Rule(ctx, c.Name)
		if err != nil {
			return nil, err
		}
		if !ok {
			rule = DefaultRule(c.Name)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// SaveRule validates and stores a category rule.
func SaveRule(ctx context.Context, rule Rule) error {
	if err := rule.Validate(); err != nil {
		return err
	}
	return db.DbSaveNotificationRule(ctx, db.NotificationRule{
		Category:        rule.Category,
		Enabled:         rule.Enabled,
		MinPushSeverity: rule.MinPushSeverity.String(),
		QuietStart:      rule.QuietStart,
		QuietEnd:        rule.QuietEnd,
	})
}

// GetNotsSince returns nots created at or after `since`.
func GetNotsSince(since time.Time) ([]db.Not, error) {
	return db.DbGetNotsFrom(context.Background(), since)
}
