package services

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

func TestNormalizeSSHPublicKeyRejectsEmpty(t *testing.T) {
	if _, err := normalizeSSHPublicKey("   "); err == nil {
		t.Fatalf("normalizeSSHPublicKey accepted an empty key")
	}
}

func TestNormalizeSSHPublicKeyRejectsInvalidBase64(t *testing.T) {
	if _, err := normalizeSSHPublicKey("ssh-ed25519 AAAA!@#$"); err == nil {
		t.Fatalf("normalizeSSHPublicKey accepted invalid key data characters")
	}
}
