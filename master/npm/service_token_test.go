package npm

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"512SvMan/internal/testnpm"
	"github.com/golang-jwt/jwt/v5"
)

func TestServiceTokenContract(t *testing.T) {
	dir := t.TempDir()
	key := testnpm.State(t, dir)
	before, err := os.ReadFile(filepath.Join(dir, "database.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	value, err := ServiceToken(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jwt.Parse(value, func(*jwt.Token) (any, error) { return &key.PublicKey, nil }, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer("api"), jwt.WithExpirationRequired())
	if err != nil {
		t.Fatal(err)
	}
	claims := parsed.Claims.(jwt.MapClaims)
	if claims["attrs"].(map[string]any)["id"] != float64(7) || claims["scope"].([]any)[0] != "user" || claims["jti"] == "" {
		t.Fatalf("unexpected service identity: %#v", claims)
	}
	expires, _ := claims.GetExpirationTime()
	if expires.Time.Before(time.Now().Add(time.Minute)) || expires.Time.After(time.Now().Add(125*time.Second)) {
		t.Fatal("service token must be short-lived")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "database.sqlite"))
	if string(before) != string(after) {
		t.Fatal("service authentication modified NPM state")
	}
	second, err := ServiceToken(context.Background(), dir)
	if err != nil || second == value {
		t.Fatal("service tokens must have independent nonce values")
	}
}

func TestServiceTokenFailsClosed(t *testing.T) {
	t.Run("missing keys", func(t *testing.T) {
		if _, err := ServiceToken(context.Background(), t.TempDir()); err == nil {
			t.Fatal("missing keys accepted")
		}
	})
	t.Run("missing database", func(t *testing.T) {
		dir := t.TempDir()
		testnpm.State(t, dir)
		path := filepath.Join(dir, "database.sqlite")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if _, err := ServiceToken(context.Background(), dir); err == nil {
			t.Fatal("missing database accepted")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("missing NPM database was created")
		}
	})
	t.Run("no active administrator", func(t *testing.T) {
		dir := t.TempDir()
		testnpm.State(t, dir)
		db, _ := sql.Open("sqlite3", filepath.Join(dir, "database.sqlite"))
		if _, err := db.Exec("UPDATE user SET is_disabled = 1 WHERE id = 7"); err != nil {
			t.Fatal(err)
		}
		db.Close()
		if _, err := ServiceToken(context.Background(), dir); err == nil {
			t.Fatal("disabled administrators accepted")
		}
	})
	t.Run("invalid keys", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "keys.json"), []byte(`{"key":"invalid-private-key"}`), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ServiceToken(context.Background(), dir); err == nil {
			t.Fatal("invalid keys accepted")
		}
	})
}
