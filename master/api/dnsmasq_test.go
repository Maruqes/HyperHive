package api

import (
	"512SvMan/dnsmasq"
	"512SvMan/services"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
)

func TestDNSAliasOptionalFields(t *testing.T) {
	oldAdd, oldVMs := dnsAliasAdd, dnsAliasVMs
	t.Cleanup(func() { dnsAliasAdd, dnsAliasVMs = oldAdd, oldVMs })
	for _, tc := range []struct {
		name, fields string
		want         dnsmasq.AliasEntry
		inventoryErr error
		status       int
	}{
		{name: "legacy payload", want: dnsmasq.AliasEntry{}, status: 200},
		{name: "explicit blanks", fields: `,"ssh_username":"","related_vm":""`, status: 200},
		{name: "username only", fields: `,"ssh_username":"manual-user"`, want: dnsmasq.AliasEntry{SSHUsername: "manual-user"}, status: 200},
		{name: "VM only", fields: `,"related_vm":"guest"`, want: dnsmasq.AliasEntry{RelatedVM: "guest"}, status: 200},
		{name: "both", fields: `,"ssh_username":"root","related_vm":"guest"`, want: dnsmasq.AliasEntry{SSHUsername: "root", RelatedVM: "guest"}, status: 200},
		{name: "unknown VM", fields: `,"related_vm":"unknown"`, status: 400},
		{name: "wrong VM type", fields: `,"related_vm":123`, status: 400},
		{name: "unavailable inventory", fields: `,"related_vm":"guest"`, inventoryErr: errors.New("slave unavailable"), status: 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got *dnsmasq.AliasEntry
			listed := false
			dnsAliasAdd = func(alias, ip, username, vm string) error {
				got = &dnsmasq.AliasEntry{Alias: alias, IP: ip, SSHUsername: username, RelatedVM: vm}
				return nil
			}
			dnsAliasVMs = func(context.Context) ([]services.VmType, []string, error) {
				listed = true
				return []services.VmType{{Vm: &grpcVirsh.Vm{Name: "guest"}}}, nil, tc.inventoryErr
			}
			recorder := httptest.NewRecorder()
			addDNSAlias(recorder, httptest.NewRequest(http.MethodPost, "/dnsmasq/alias/add", strings.NewReader(`{"alias":"guest.test","ip":"192.0.2.1"`+tc.fields+`}`)))
			if recorder.Code != tc.status {
				t.Fatalf("HTTP %d, want %d: %s", recorder.Code, tc.status, recorder.Body.String())
			}
			if tc.status != 200 {
				if got != nil {
					t.Fatal("invalid request persisted")
				}
				return
			}
			want := tc.want
			want.Alias, want.IP = "guest.test", "192.0.2.1"
			if got == nil || *got != want {
				t.Fatalf("stored %+v, want %+v", got, want)
			}
			if listed != (want.RelatedVM != "") {
				t.Fatal("VM lookup must be optional")
			}
		})
	}
}

func TestDNSAliasListReturnsOptionalFields(t *testing.T) {
	old := dnsAliasList
	t.Cleanup(func() { dnsAliasList = old })
	dnsAliasList = func() ([]dnsmasq.AliasEntry, error) {
		return []dnsmasq.AliasEntry{{Alias: "legacy.test", IP: "192.0.2.1"}, {Alias: "new.test", IP: "192.0.2.2", SSHUsername: "user", RelatedVM: "guest"}}, nil
	}
	recorder := httptest.NewRecorder()
	getAllDNSAliases(recorder, httptest.NewRequest(http.MethodGet, "/dnsmasq/alias/all", nil))
	var payload struct {
		Aliases []map[string]string `json:"aliases"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	want := []map[string]string{
		{"alias": "legacy.test", "ip": "192.0.2.1", "ssh_username": "", "related_vm": ""},
		{"alias": "new.test", "ip": "192.0.2.2", "ssh_username": "user", "related_vm": "guest"},
	}
	if recorder.Code != 200 || !reflect.DeepEqual(payload.Aliases, want) {
		t.Fatalf("unexpected response: %s", recorder.Body.String())
	}
}
