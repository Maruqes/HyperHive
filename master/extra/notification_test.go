package extra

import (
	"testing"

	"512SvMan/nots"

	extraGrpc "github.com/Maruqes/512SvMan/api/proto/extra"
)

func TestNotificationFromSlaveMapsLegacyAndTyped(t *testing.T) {
	legacy := notificationFromSlave(&extraGrpc.Notification{Title: "x", Critical: true})
	if legacy.Severity != nots.SeverityError || legacy.Event != nots.CategoryLegacy {
		t.Fatalf("legacy critical: %+v", legacy)
	}
	if n := notificationFromSlave(&extraGrpc.Notification{Title: "x"}); n.Severity != nots.SeverityInfo {
		t.Fatalf("legacy info: %+v", n)
	}
	typed := notificationFromSlave(&extraGrpc.Notification{Title: "x", Critical: true, Severity: extraGrpc.NotificationSeverity_SEVERITY_WARNING, Event: "health.smart", DedupKey: "k", Machine: "node1"})
	if typed.Severity != nots.SeverityWarning || typed.Event != "health.smart" || typed.DedupKey != "k" || typed.Machine != "node1" {
		t.Fatalf("typed: %+v", typed)
	}
}
