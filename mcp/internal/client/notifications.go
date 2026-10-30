package client

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"
)

const NotificationRulesPath = "/notification/rules"

// NotificationRule is the delivery policy of one notification category.
type NotificationRule struct {
	Category        string `json:"category"`
	Description     string `json:"description"`
	Enabled         bool   `json:"enabled"`
	MinPushSeverity string `json:"minPushSeverity"`
	QuietStart      string `json:"quietStart"`
	QuietEnd        string `json:"quietEnd"`
}

func (c *Client) ListNotificationRules(ctx context.Context) ([]NotificationRule, error) {
	var rules []NotificationRule
	if err := c.getJSON(ctx, NotificationRulesPath, &rules); err != nil {
		return nil, err
	}
	if rules == nil {
		return nil, errors.New("HyperHive API returned an invalid notification rules array")
	}
	return rules, nil
}

// UpdateNotificationRuleInput fully replaces one category's rule.
type UpdateNotificationRuleInput struct {
	Category        string `json:"category" jsonschema:"Category from list_notification_rules: connection, health, metrics, operation, vm, system or legacy"`
	Enabled         bool   `json:"enabled" jsonschema:"false keeps notifications in history only: no websocket, web push or Android push"`
	MinPushSeverity string `json:"minPushSeverity" jsonschema:"Lowest severity sent as web and Android push: info, warning, error or critical"`
	QuietStart      string `json:"quietStart,omitempty" jsonschema:"Optional HH:MM (master local time) when non-critical pushes stop; requires quietEnd"`
	QuietEnd        string `json:"quietEnd,omitempty" jsonschema:"Optional HH:MM when quiet hours end; requires quietStart"`
}

func ValidateUpdateNotificationRule(input UpdateNotificationRuleInput) error {
	switch input.Category {
	case "connection", "health", "metrics", "operation", "vm", "system", "legacy":
	default:
		return fmt.Errorf("unknown notification category %q", input.Category)
	}
	switch input.MinPushSeverity {
	case "info", "warning", "error", "critical":
	default:
		return errors.New("minPushSeverity must be info, warning, error or critical")
	}
	if (input.QuietStart == "") != (input.QuietEnd == "") {
		return errors.New("quietStart and quietEnd must be set together")
	}
	for _, v := range []string{input.QuietStart, input.QuietEnd} {
		if _, err := time.Parse("15:04", v); v != "" && err != nil {
			return fmt.Errorf("invalid quiet hours time %q (use HH:MM)", v)
		}
	}
	return nil
}

func (c *Client) UpdateNotificationRule(ctx context.Context, input UpdateNotificationRuleInput) (NotificationRule, error) {
	if err := ValidateUpdateNotificationRule(input); err != nil {
		return NotificationRule{}, err
	}
	body := map[string]any{"enabled": input.Enabled, "minPushSeverity": input.MinPushSeverity, "quietStart": input.QuietStart, "quietEnd": input.QuietEnd}
	var rule NotificationRule
	if err := c.request(ctx, "PUT", NotificationRulesPath+"/"+url.PathEscape(input.Category), body, &rule); err != nil {
		return NotificationRule{}, err
	}
	if rule.Category != input.Category {
		return NotificationRule{}, errors.New("HyperHive API returned a rule for a different category")
	}
	return rule, nil
}
