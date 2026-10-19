package dnsmasq

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func aliasFixture(t *testing.T, config string) *int {
	t.Helper()
	oldPath, oldInclude, oldReload := aliasConfPath, ensureAliasInclude, reloadAliases
	aliasConfPath = filepath.Join(t.TempDir(), "aliases.conf")
	if err := os.WriteFile(aliasConfPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	reloads := new(int)
	ensureAliasInclude = func() error { return nil }
	reloadAliases = func() error { *reloads++; return nil }
	t.Cleanup(func() { aliasConfPath, ensureAliasInclude, reloadAliases = oldPath, oldInclude, oldReload })
	return reloads
}

func TestAliasOptionalMetadataPersistence(t *testing.T) {
	aliasFixture(t, "# Existing config\naddress=/legacy.test/192.0.2.1\n")
	entries, err := GetAllAliases()
	if err != nil || !reflect.DeepEqual(entries, []AliasEntry{{Alias: "legacy.test", IP: "192.0.2.1"}}) {
		t.Fatalf("legacy records: %+v, %v", entries, err)
	}
	username := "user\"\\\naddress=/injected.test/192.0.2.99"
	if err := AddAlias("new.test", "192.0.2.2", username, "guest-one"); err != nil {
		t.Fatal(err)
	}
	entries, err = GetAllAliases()
	if err != nil || len(entries) != 2 || entries[1].SSHUsername != username || entries[1].RelatedVM != "guest-one" {
		t.Fatalf("metadata round trip: %+v, %v", entries, err)
	}
	config, _ := os.ReadFile(aliasConfPath)
	if strings.Contains(string(config), "\naddress=/injected.test/") {
		t.Fatal("manual metadata escaped its comment")
	}
	// Editing only metadata must work even when alias and IP stay the same.
	if err := AddAlias("new.test", "192.0.2.2", "other-user", "guest-two"); err != nil {
		t.Fatal(err)
	}
	entries, _ = GetAllAliases()
	if entries[1].SSHUsername != "other-user" || entries[1].RelatedVM != "guest-two" {
		t.Fatalf("metadata-only edit ignored: %+v", entries)
	}
	if err := AddAlias("new.test", "192.0.2.3", "", ""); err != nil {
		t.Fatal(err)
	}
	entries, _ = GetAllAliases()
	if entries[1] != (AliasEntry{Alias: "new.test", IP: "192.0.2.3"}) || entries[0].RelatedVM != "" {
		t.Fatalf("clearing metadata or legacy preservation failed: %+v", entries)
	}
}

func TestRemoveAliasesForVM(t *testing.T) {
	reloads := aliasFixture(t, "address=/legacy.test/192.0.2.1\n")
	for _, e := range []AliasEntry{
		{Alias: "one.test", IP: "192.0.2.1", SSHUsername: "root", RelatedVM: "guest"},
		{Alias: "two.test", IP: "192.0.2.2", RelatedVM: "guest"},
		{Alias: "other.test", IP: "192.0.2.1", SSHUsername: "admin", RelatedVM: "other-guest"},
	} {
		if err := AddAlias(e.Alias, e.IP, e.SSHUsername, e.RelatedVM); err != nil {
			t.Fatal(err)
		}
	}
	before := *reloads
	if err := RemoveAliasesForVM("guest"); err != nil {
		t.Fatal(err)
	}
	entries, err := GetAllAliases()
	want := []AliasEntry{{Alias: "legacy.test", IP: "192.0.2.1"}, {Alias: "other.test", IP: "192.0.2.1", SSHUsername: "admin", RelatedVM: "other-guest"}}
	if err != nil || !reflect.DeepEqual(entries, want) || *reloads != before+1 {
		t.Fatalf("cascade should delete all and only linked records in one reload: %+v, %v, reloads=%d", entries, err, *reloads)
	}
	if err := RemoveAliasesForVM(""); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAliasesForVM("guest"); err != nil || *reloads != before+1 {
		t.Fatalf("empty/missing VM should not change config: %v", err)
	}
	if err := RemoveAlias("other.test", "192.0.2.1"); err != nil {
		t.Fatal(err)
	}
	entries, _ = GetAllAliases()
	if len(entries) != 1 || entries[0].Alias != "legacy.test" {
		t.Fatalf("manual deletion failed: %+v", entries)
	}
}

func TestAliasCleanupFailures(t *testing.T) {
	aliasFixture(t, "")
	if err := AddAlias("one.test", "192.0.2.1", "root", "guest"); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("reload unavailable")
	reloadAliases = func() error { return wantErr }
	if err := RemoveAliasesForVM("guest"); !errors.Is(err, wantErr) {
		t.Fatalf("reload error lost: %v", err)
	}
	entries, _ := GetAllAliases()
	if len(entries) != 0 {
		t.Fatal("config was not updated before reload")
	}
	if err := os.WriteFile(aliasConfPath, []byte(metadataPrefix+"{invalid}\naddress=/one.test/192.0.2.1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAliasesForVM("guest"); err == nil {
		t.Fatal("corrupt metadata must not silently lose VM relationships")
	}
	aliasConfPath = t.TempDir()
	if err := RemoveAliasesForVM("guest"); err == nil {
		t.Fatal("read failure lost")
	}
}

func TestConcurrentAliasWritesPreserveRecords(t *testing.T) {
	aliasFixture(t, "")
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := AddAlias(fmt.Sprintf("guest-%d.test", i), "192.0.2.1", "user", "guest"); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	entries, err := GetAllAliases()
	if err != nil || len(entries) != 12 {
		t.Fatalf("concurrent updates lost: %d entries, %v", len(entries), err)
	}
}

func TestAliasMetadataPreservesMultidomainAliases(t *testing.T) {
	aliasFixture(t, "")
	if err := AddAlias("one.test/two.test", "2001:db8::1", "user", "guest"); err != nil {
		t.Fatal(err)
	}
	entries, err := GetAllAliases()
	want := []AliasEntry{{Alias: "one.test/two.test", IP: "2001:db8::1", SSHUsername: "user", RelatedVM: "guest"}}
	if err != nil || !reflect.DeepEqual(entries, want) {
		t.Fatalf("multidomain alias lost metadata: %+v, %v", entries, err)
	}
}
