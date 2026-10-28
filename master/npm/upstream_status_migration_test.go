package npm

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateLegacyStatusListeners(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for the NPM migration check")
	}
	root := t.TempDir()
	old := "server { listen unix:/tmp/hyperhive-upstream-status.sock; location = /generation { return 200 \"12\"; } }\n"
	for _, name := range []string{"10", "12", "unmanaged"} {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "http.conf"), []byte(old), 0644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(root string) {
		t.Helper()
		cmd := exec.Command(node, "-e", migrateLegacyStatusListenersJS)
		cmd.Stdin = strings.NewReader(`{"root":"` + root + `"}`)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("migration: %v: %s", err, out)
		}
	}
	run(root)
	run(root)
	run(filepath.Join(root, "missing"))
	for _, name := range []string{"10", "12", "unmanaged"} {
		file := filepath.Join(root, name, "http.conf")
		actual, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		want := old
		if name != "unmanaged" {
			want = strings.ReplaceAll(old, "listen unix:/tmp/hyperhive-upstream-status.sock;", "listen 127.0.0.1:18959;")
			backup, err := os.ReadFile(file + ".before-status-tcp")
			if err != nil || string(backup) != old {
				t.Fatalf("backup lost: %q %v", backup, err)
			}
		}
		if string(actual) != want {
			t.Fatalf("%s: %q", name, actual)
		}
	}
}
