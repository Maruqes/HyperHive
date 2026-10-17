package celld

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstalledBinaryExposedOnPath(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "official-celld")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho 'celld 0.6.2'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "celld")
	m := &Manager{binary: binary, commandLink: link}
	for i := 0; i < 2; i++ {
		// An already installed release must still create/repair its PATH entry.
		if err := m.install(context.Background(), "v0.6.2"); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("/bin/sh", "-c", "celld --version")
	cmd.Env = append(os.Environ(), "PATH="+dir)
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "celld 0.6.2" {
		t.Fatalf("run from reduced PATH: %s, %v", out, err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := m.install(context.Background(), "v0.6.2"); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(link); err != nil || target != binary {
		t.Fatalf("repaired command: %q, %v", target, err)
	}
}

func TestCommandPreservesOtherInstallation(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "symlink"}[symlink], func(t *testing.T) {
			link := filepath.Join(t.TempDir(), "celld")
			var err error
			if symlink {
				err = os.Symlink("/other/celld", link)
			} else {
				err = os.WriteFile(link, []byte("other installation"), 0755)
			}
			if err != nil {
				t.Fatal(err)
			}
			m := &Manager{binary: Binary, commandLink: link}
			if err := m.ensureCommandLink(); err == nil {
				t.Fatal("accepted conflicting installation")
			}
			if symlink {
				if target, err := os.Readlink(link); err != nil || target != "/other/celld" {
					t.Fatalf("changed existing symlink: %q, %v", target, err)
				}
			} else if data, err := os.ReadFile(link); err != nil || string(data) != "other installation" {
				t.Fatalf("changed existing file: %q, %v", data, err)
			}
		})
	}
}
