package secretbox

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSealOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keyring")
	b, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("key file mode %v", info.Mode().Perm())
	}
	secret := []byte("sk-ant-test-secret")
	sealed := b.Seal(secret, "user:1:ai")
	if bytes.Contains(sealed, secret) {
		t.Fatal("plaintext visible in sealed value")
	}
	if b.Seal(secret, "user:1:ai")[0] == sealed[0] && bytes.Equal(b.Seal(secret, "user:1:ai"), sealed) {
		t.Error("sealing is deterministic")
	}
	// The same key file opens it after a restart.
	again, _ := Load(path)
	if got, err := again.Open(sealed, "user:1:ai"); err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("%q %v", got, err)
	}
	// Bound to its context; tampering and other keys fail.
	if _, err := again.Open(sealed, "user:2:ai"); !errors.Is(err, ErrUnreadable) {
		t.Errorf("other context: %v", err)
	}
	tampered := bytes.Clone(sealed)
	tampered[len(tampered)-1] ^= 1
	if _, err := again.Open(tampered, "user:1:ai"); !errors.Is(err, ErrUnreadable) {
		t.Errorf("tampered: %v", err)
	}
	other, _ := Load(filepath.Join(t.TempDir(), "keyring"))
	if _, err := other.Open(sealed, "user:1:ai"); !errors.Is(err, ErrUnreadable) {
		t.Errorf("other key: %v", err)
	}
	if _, err := other.Open([]byte("x"), "user:1:ai"); !errors.Is(err, ErrUnreadable) {
		t.Errorf("short: %v", err)
	}
}

func TestBadKeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keyring")
	os.WriteFile(path, []byte("short"), 0o600)
	if _, err := Load(path); err == nil {
		t.Error("short key accepted")
	}
}
