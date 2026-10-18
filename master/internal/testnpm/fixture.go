// Package testnpm provides disposable local NPM state for authentication tests.
package testnpm

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func State(t *testing.T, dir string) *rsa.PrivateKey {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	data, _ := json.Marshal(map[string]string{"key": string(keyPEM)})
	if err := os.WriteFile(filepath.Join(dir, "keys.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", filepath.Join(dir, "database.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE user (id INTEGER PRIMARY KEY, roles TEXT, is_deleted INTEGER, is_disabled INTEGER);
INSERT INTO user VALUES (1, '["admin"]', 1, 0), (2, '["admin"]', 0, 1), (3, '["user"]', 0, 0), (7, '["admin"]', 0, 0);`)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
