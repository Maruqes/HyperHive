package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"512SvMan/nots"
)

type notificationRuleDTO struct {
	Category        string `json:"category"`
	Description     string `json:"description"`
	Enabled         bool   `json:"enabled"`
	MinPushSeverity string `json:"minPushSeverity"`
	QuietStart      string `json:"quietStart"`
	QuietEnd        string `json:"quietEnd"`
}

func ruleDTO(rule nots.Rule) notificationRuleDTO {
	dto := notificationRuleDTO{
		Category:        rule.Category,
		Enabled:         rule.Enabled,
		MinPushSeverity: rule.MinPushSeverity.String(),
		QuietStart:      rule.QuietStart,
		QuietEnd:        rule.QuietEnd,
	}
	for _, c := range nots.Categories {
		if c.Name == rule.Category {
			dto.Description = c.Description
		}
	}
	return dto
}

type notificationRuleStore struct {
	list func(context.Context) ([]nots.Rule, error)
	save func(context.Context, nots.Rule) error
}

var liveNotificationRules = notificationRuleStore{list: nots.GetRules, save: nots.SaveRule}

// GET /notification/rules returns the effective rule of every category (stored or default).
func (s notificationRuleStore) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.list(r.Context())
	if err != nil {
		http.Error(w, "failed to load notification rules", http.StatusInternalServerError)
		return
	}
	out := make([]notificationRuleDTO, 0, len(rules))
	for _, rule := range rules {
		out = append(out, ruleDTO(rule))
	}
	writeJSONWithStatus(w, http.StatusOK, out)
}

// PUT /notification/rules/{category} replaces one category's rule. All fields are required
// except quiet hours; send both quietStart and quietEnd as "" to disable quiet hours.
func (s notificationRuleStore) putRule(w http.ResponseWriter, r *http.Request) {
	category := chi.URLParam(r, "category")
	if !nots.IsCategory(category) {
		http.Error(w, "unknown notification category", http.StatusNotFound)
		return
	}
	var input struct {
		Enabled         *bool   `json:"enabled"`
		MinPushSeverity *string `json:"minPushSeverity"`
		QuietStart      string  `json:"quietStart"`
		QuietEnd        string  `json:"quietEnd"`
	}
	if !decodeStrict(w, r, &input) || input.Enabled == nil || input.MinPushSeverity == nil {
		http.Error(w, "body needs enabled and minPushSeverity", http.StatusBadRequest)
		return
	}
	severity, err := nots.ParseSeverity(*input.MinPushSeverity)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rule := nots.Rule{Category: category, Enabled: *input.Enabled, MinPushSeverity: severity, QuietStart: input.QuietStart, QuietEnd: input.QuietEnd}
	if err := rule.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.save(r.Context(), rule); err != nil {
		http.Error(w, "failed to save notification rule", http.StatusInternalServerError)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, ruleDTO(rule))
}
