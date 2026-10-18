package npm

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/golang-jwt/jwt/v5"
	_ "github.com/mattn/go-sqlite3"
)

// ServiceToken bridges HyperHive's static MCP credential to the locally managed
// NPM instance. It reads existing state without creating users or modifying NPM.
// The claims match the pinned NPM 2.13.5 token and access implementations.
func ServiceToken(ctx context.Context, dataDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dataDir, "keys.json"))
	if err != nil {
		return "", errors.New("NPM signing key is unavailable")
	}
	var keys struct {
		Key string `json:"key"`
	}
	if json.Unmarshal(data, &keys) != nil {
		return "", errors.New("NPM signing key is invalid")
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(keys.Key))
	if err != nil {
		return "", errors.New("NPM signing key is invalid")
	}
	path, err := filepath.Abs(filepath.Join(dataDir, "database.sqlite"))
	if err != nil {
		return "", errors.New("NPM user database is unavailable")
	}
	u := url.URL{Scheme: "file", Path: path}
	q := url.Values{"mode": {"ro"}, "_query_only": {"1"}, "_busy_timeout": {"1000"}}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return "", errors.New("NPM user database is unavailable")
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT id, roles FROM user WHERE is_deleted = 0 AND is_disabled = 0 ORDER BY id`)
	if err != nil {
		return "", errors.New("NPM user database is unavailable")
	}
	defer rows.Close()
	var adminID int64
	for rows.Next() {
		var id int64
		var rolesJSON string
		if rows.Scan(&id, &rolesJSON) != nil {
			return "", errors.New("NPM user data is invalid")
		}
		var roles []string
		if json.Unmarshal([]byte(rolesJSON), &roles) != nil {
			return "", errors.New("NPM user roles are invalid")
		}
		if id > 0 && slices.Contains(roles, "admin") {
			adminID = id
			break
		}
	}
	if err := rows.Err(); err != nil {
		return "", errors.New("NPM user database is unavailable")
	}
	if adminID == 0 {
		return "", errors.New("NPM has no active administrator")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", errors.New("could not create NPM service credential")
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": "api", "attrs": map[string]any{"id": adminID}, "scope": []string{"user"},
		"iat": now.Unix(), "exp": now.Add(2 * time.Minute).Unix(), "jti": hex.EncodeToString(nonce[:]),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
	if err != nil {
		return "", errors.New("could not sign NPM service credential")
	}
	return token, nil
}
