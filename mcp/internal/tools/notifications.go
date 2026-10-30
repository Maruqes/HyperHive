package tools

import (
	"context"

	"github.com/Maruqes/HyperHive/mcp/internal/client"
	"github.com/Maruqes/HyperHive/mcp/internal/safety"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NotificationsAPI exposes notification delivery rules. There is no deletion.
type NotificationsAPI interface {
	ListNotificationRules(context.Context) ([]client.NotificationRule, error)
	UpdateNotificationRule(context.Context, client.UpdateNotificationRuleInput) (client.NotificationRule, error)
}

type notificationRulesOutput struct {
	Result []client.NotificationRule `json:"result"`
}

type notificationRuleOutput struct {
	Result client.NotificationRule `json:"result"`
}

func notificationsTool(name, method, path, description string) *mcp.Tool {
	destructive := method == "PUT"
	return &mcp.Tool{Name: name, Description: description, Meta: mcp.Meta{"hyperhive/endpoint": map[string]any{"method": method, "path": path}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: method == "GET", DestructiveHint: &destructive, IdempotentHint: true}}
}

func defineListNotificationRules(api NotificationsAPI) ToolDefinition {
	return defineTool(notificationsTool("list_notification_rules", "GET", client.NotificationRulesPath, "List the effective notification delivery rule of every category (stored or default). Calls GET /notification/rules. Takes no arguments."), func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, notificationRulesOutput, error) {
		rules, err := api.ListNotificationRules(ctx)
		return nil, notificationRulesOutput{rules}, err
	})
}

func defineUpdateNotificationRule(api NotificationsAPI) ToolDefinition {
	return defineTool(notificationsTool("update_notification_rule", "PUT", client.NotificationRulesPath+"/{category}", "Replace one category's notification rule. Calls PUT /notification/rules/{category}. All of enabled and minPushSeverity are required; omitted quiet hours disable them. Quiet hours hold back pushes below critical; history is always kept. Read the current rule with list_notification_rules first. No automatic retries."), func(ctx context.Context, _ *mcp.CallToolRequest, input client.UpdateNotificationRuleInput) (*mcp.CallToolResult, notificationRuleOutput, error) {
		if err := client.ValidateUpdateNotificationRule(input); err != nil {
			return nil, notificationRuleOutput{}, err
		}
		rule, err := api.UpdateNotificationRule(ctx, input)
		return nil, notificationRuleOutput{rule}, err
	})
}

// RegisterNotifications is the complete tool and safety catalog for this area.
func RegisterNotifications(server *Registry, api NotificationsAPI) {
	server = server.Area("notifications")
	RegisterTool(server, "list_notification_rules", defineListNotificationRules(api), safety.Low)
	RegisterTool(server, "update_notification_rule", defineUpdateNotificationRule(api), safety.Medium)
}
