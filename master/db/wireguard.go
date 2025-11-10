package db

import (
	"database/sql"
	"fmt"
)

// WireguardPeer represents a row in the wireguard_peers table.
type WireguardPeer struct {
	Id        int
	Name      string
	ClientIP  string
	PublicKey string
}

// CreateWireguardPeerTable ensures the wireguard_peers table exists.
func CreateWireguardPeerTable() error {
	const query = `
	CREATE TABLE IF NOT EXISTS wireguard_peers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		client_ip TEXT NOT NULL,
		public_key TEXT NOT NULL DEFAULT ''
	);
	`
	if _, err := DB.Exec(query); err != nil {
		return err
	}
	return nil
}

// InsertWireguardPeer inserts a new peer record and returns its ID.
func InsertWireguardPeer(name, clientIP, publicKey string) (int, error) {
	const query = `
	INSERT INTO wireguard_peers (name, client_ip, public_key)
	VALUES (?, ?, ?);
	`
	result, err := DB.Exec(query, name, clientIP, publicKey)
	if err != nil {
		return 0, fmt.Errorf("insert wireguard peer: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("wireguard peer last insert id: %w", err)
	}
	return int(id), nil
}

// GetWireguardPeerByID fetches a peer by its ID.
func GetWireguardPeerByID(id int) (*WireguardPeer, error) {
	const query = `
	SELECT id, name, client_ip, public_key
	FROM wireguard_peers
	WHERE id = ?;
	`
	var peer WireguardPeer
	err := DB.QueryRow(query, id).Scan(&peer.Id, &peer.Name, &peer.ClientIP, &peer.PublicKey)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get wireguard peer by id: %w", err)
	}
	return &peer, nil
}

// GetAllWireguardPeers returns every peer stored in the table.
func GetAllWireguardPeers() ([]WireguardPeer, error) {
	const query = `
	SELECT id, name, client_ip, public_key
	FROM wireguard_peers
	ORDER BY id ASC;
	`
	rows, err := DB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("get all wireguard peers: %w", err)
	}
	defer rows.Close()

	var peers []WireguardPeer
	for rows.Next() {
		var peer WireguardPeer
		if err := rows.Scan(&peer.Id, &peer.Name, &peer.ClientIP, &peer.PublicKey); err != nil {
			return nil, fmt.Errorf("scan wireguard peer: %w", err)
		}
		peers = append(peers, peer)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate wireguard peers: %w", err)
	}
	return peers, nil
}

// UpdateWireguardPeer updates the name/client IP for a given peer ID.
func UpdateWireguardPeer(id int, name, clientIP, publicKey string) error {
	const query = `
	UPDATE wireguard_peers
	SET name = ?, client_ip = ?, public_key = ?
	WHERE id = ?;
	`
	_, err := DB.Exec(query, name, clientIP, publicKey, id)
	if err != nil {
		return fmt.Errorf("update wireguard peer: %w", err)
	}
	return nil
}

// DeleteWireguardPeer removes a peer by ID.
func DeleteWireguardPeer(id int) error {
	const query = `
	DELETE FROM wireguard_peers
	WHERE id = ?;
	`
	_, err := DB.Exec(query, id)
	if err != nil {
		return fmt.Errorf("delete wireguard peer: %w", err)
	}
	return nil
}

// DeleteAllWireguardPeers truncates the table.
func DeleteAllWireguardPeers() error {
	const query = `
	DELETE FROM wireguard_peers;
	`
	if _, err := DB.Exec(query); err != nil {
		return fmt.Errorf("delete all wireguard peers: %w", err)
	}
	return nil
}
