package virsh

import "testing"

func TestNormalizeSSHPublicKeyDropsComment(t *testing.T) {
	got, err := normalizeSSHPublicKey("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey user@example")
	if err != nil {
		t.Fatalf("normalizeSSHPublicKey returned error: %v", err)
	}

	want := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey"
	if got != want {
		t.Fatalf("normalizeSSHPublicKey = %q, want %q", got, want)
	}
}

func TestNormalizeSSHPublicKeyRejectsMultiline(t *testing.T) {
	if _, err := normalizeSSHPublicKey("ssh-ed25519 AAAA\nssh-ed25519 BBBB"); err == nil {
		t.Fatalf("normalizeSSHPublicKey accepted a multiline key")
	}
}

func TestNormalizeSSHPublicKeyRejectsUnsupportedType(t *testing.T) {
	if _, err := normalizeSSHPublicKey("sk-ssh-ed25519@openssh.com AAAA"); err == nil {
		t.Fatalf("normalizeSSHPublicKey accepted an unsupported key type")
	}
}

func TestShellSingleQuote(t *testing.T) {
	got := shellSingleQuote("a'b")
	want := "'a'\"'\"'b'"
	if got != want {
		t.Fatalf("shellSingleQuote = %q, want %q", got, want)
	}
}
