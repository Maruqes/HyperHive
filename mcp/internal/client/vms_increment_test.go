package client

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"golang.org/x/crypto/ssh"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVMIncrementValidation(t *testing.T) {
	valid := VMScheduleSettings{VMName: "guest", FrequencyDays: 1, MinTime: "23:30", MaxTime: "00:15", NFSMountID: 2, MaxBackupsRetain: 3}
	for _, clocks := range [][2]string{{"23:30", "00:15"}, {"01:00", "01:45"}, {"00:00", "00:00"}} {
		v := valid
		v.MinTime = clocks[0]
		v.MaxTime = clocks[1]
		if ValidateVMWrite(v) != nil {
			t.Errorf("valid window rejected %v", clocks)
		}
	}
	for _, clocks := range [][2]string{{"12:00", "12:44"}, {"24:00", "01:00"}, {"1:00", "02:00"}, {"01:60", "02:00"}} {
		v := valid
		v.MinTime = clocks[0]
		v.MaxTime = clocks[1]
		if ValidateVMWrite(v) == nil {
			t.Errorf("invalid window accepted %v", clocks)
		}
	}
	for _, field := range []string{"frequency", "nfs", "retention"} {
		v := valid
		switch field {
		case "frequency":
			v.FrequencyDays = 0
		case "nfs":
			v.NFSMountID = 0
		case "retention":
			v.MaxBackupsRetain = 0
		}
		if ValidateVMWrite(v) == nil {
			t.Errorf("invalid %s accepted", field)
		}
	}
	key, _ := ssh.NewPublicKey(ed25519.NewKeyFromSeed(make([]byte, 32)).Public())
	public := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	if ValidateVMWrite(VMSSHInput{VMName: "guest", VMSSHSettings: VMSSHSettings{SSHKey: public + " comment"}}) != nil {
		t.Fatal("valid public key rejected")
	}
	for _, bad := range []string{"", "-----BEGIN PRIVATE KEY-----", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5", "ssh-ed25519 not-base64", public + "\n" + public, "command=\"touch /tmp/pwn\" " + public, "ssh-rsa " + strings.Fields(public)[1]} {
		if ValidateVMWrite(VMSSHInput{VMName: "guest", VMSSHSettings: VMSSHSettings{SSHKey: bad}}) == nil {
			t.Errorf("invalid key accepted %q", bad)
		}
	}
}
func TestVMProvisioningTimeoutAndCancellation(t *testing.T) {
	var calls int
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(200) }))
	defer backend.Close()
	c, err := New(Config{BaseURL: backend.URL, Token: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	slow := c.withVMProvisioningTimeout()
	if slow.http.Timeout != 15*time.Minute || c.http.Timeout != 30*time.Second || slow.http.CheckRedirect == nil {
		t.Fatal("provisioning changed shared transport protections")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.SetVMAutostart(ctx, VMAutostartInput{VMName: "guest", VMAutostartSettings: VMAutostartSettings{AutoStart: false}})
	if err == nil || calls != 0 {
		t.Fatal("canceled mutation reached backend")
	}
}
func TestVMCreateValidation(t *testing.T) {
	var valid VMCreateInput
	if json.Unmarshal([]byte(`{"machine_name":"host","name":"guest","memory":1024,"vcpu":2,"disk_sizeGB":10,"template_id":0,"iso_id":8,"nfs_share_id":2,"network":"custom-network","VNC_password":"","cpu_xml":"","live":false,"auto_start":false,"is_windows":false}`), &valid) != nil {
		t.Fatal("fixture")
	}
	if ValidateVMWrite(valid) != nil {
		t.Fatal("valid creation rejected")
	}
	template := valid
	template.TemplateID = 7
	template.ISOID = 0
	template.Memory = 0
	template.VCPU = 0
	template.Network = ""
	if ValidateVMWrite(template) != nil {
		t.Fatal("template defaults rejected")
	}
	for _, field := range []string{"name", "disk", "iso", "memory", "vcpu", "nfs", "cpu"} {
		v := valid
		switch field {
		case "name":
			v.Name = "../escape"
		case "disk":
			v.DiskSizeGB = 0
		case "iso":
			v.ISOID = 0
		case "memory":
			v.Memory = 0
		case "vcpu":
			v.VCPU = 0
		case "nfs":
			v.NFSShareID = 0
		case "cpu":
			v.CPUXML = "<domain/>"
		}
		if ValidateVMWrite(v) == nil {
			t.Errorf("invalid %s accepted", field)
		}
	}
}
