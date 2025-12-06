package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"512SvMan/db"
	"512SvMan/env512"
	"512SvMan/wireguard"

	"github.com/go-chi/chi/v5"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type createPeerRequest struct {
	Name             string `json:"name"`
	Endpoint         string `json:"endpoint,omitempty"`
	KeepaliveSeconds *int   `json:"keepalive_seconds,omitempty"`
}

type peerPayload struct {
	ID        int          `json:"id"`
	Name      string       `json:"name"`
	ClientIP  string       `json:"client_ip"`
	PublicKey string       `json:"public_key"`
	WGPeer    wgtypes.Peer `json:"wireguard"`
	Config    string       `json:"config"`
}

type createPeerResponse struct {
	Peer     peerPayload `json:"peer"`
	Config   string      `json:"config"`
	Endpoint string      `json:"endpoint"`
}

func matchWGPeer(clientCIDR string, peers []wgtypes.Peer) (wgtypes.Peer, bool) {
	for i := range peers {
		for _, allowed := range peers[i].AllowedIPs {
			if allowed.String() == clientCIDR {
				return peers[i], true
			}
		}
	}
	return wgtypes.Peer{}, false
}

func createVPN(w http.ResponseWriter, r *http.Request) {
	if err := wireguard.RemoveAllPeers(); err != nil {
		http.Error(w, fmt.Sprintf("reset wireguard peers: %v", err), http.StatusInternalServerError)
		return
	}

	if err := db.DeleteAllWireguardPeers(); err != nil {
		http.Error(w, fmt.Sprintf("truncate wireguard peers: %v", err), http.StatusInternalServerError)
		return
	}

	if err := wireguard.SetupInterface(); err != nil {
		http.Error(w, fmt.Sprintf("setup wireguard: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "wireguard interface ready"})
}

func newPeer(w http.ResponseWriter, r *http.Request) {
	var req createPeerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "peer name is required", http.StatusBadRequest)
		return
	}

	endpoint := strings.TrimSpace(req.Endpoint)
	if endpoint == "" {
		if env512.MASTER_INTERNET_IP == "" {
			http.Error(w, "wireguard endpoint not configured", http.StatusInternalServerError)
			return
		}
		endpoint = fmt.Sprintf("%s:%d", env512.MASTER_INTERNET_IP, wireguard.ListenPort())
	}

	clientIP, err := wireguard.NextAvailableClientIP()
	if err != nil {
		http.Error(w, fmt.Sprintf("select client ip: %v", err), http.StatusInternalServerError)
		return
	}
	clientCIDR := clientIP + "/32"

	keepalive := 25
	if req.KeepaliveSeconds != nil && *req.KeepaliveSeconds > 0 {
		keepalive = *req.KeepaliveSeconds
	}

	config, clientPubKey, err := wireguard.GeneratePeerAndGenerateConfig(clientCIDR, endpoint, keepalive)
	if err != nil {
		http.Error(w, fmt.Sprintf("generate config: %v", err), http.StatusInternalServerError)
		return
	}

	_, err = db.InsertWireguardPeer(req.Name, clientCIDR, clientPubKey)
	if err != nil {
		http.Error(w, fmt.Sprintf("persist peer: %v", err), http.StatusInternalServerError)
		return
	}

	filename := strings.TrimSpace(req.Name)
	if filename == "" {
		filename = "wireguard"
	}
	filename = strings.ReplaceAll(filename, " ", "-") + ".conf"

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	_, _ = w.Write([]byte(config))
}

func getPeers(w http.ResponseWriter, r *http.Request) {
	peers, err := db.GetAllWireguardPeers()
	if err != nil {
		http.Error(w, fmt.Sprintf("list peers: %v", err), http.StatusInternalServerError)
		return
	}

	wgPeers, err := wireguard.GetPeers()
	if err != nil {
		http.Error(w, fmt.Sprintf("read wireguard peers: %v", err), http.StatusInternalServerError)
		return
	}

	payload := make([]peerPayload, 0, len(peers))
	for _, peer := range peers {

		item := peerPayload{
			ID:        peer.Id,
			Name:      peer.Name,
			ClientIP:  peer.ClientIP,
			PublicKey: peer.PublicKey,
		}
		if wgPeer, ok := matchWGPeer(peer.ClientIP, wgPeers); ok {
			item.WGPeer = wgPeer
		}

		payload = append(payload, item)
	}

	writeJSON(w, map[string]any{"peers": payload})
}

func deletePeer(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "id")
	if strings.TrimSpace(rawID) == "" {
		http.Error(w, "peer id is required", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(rawID)
	if err != nil {
		http.Error(w, "peer id must be numeric", http.StatusBadRequest)
		return
	}

	peer, err := db.GetWireguardPeerByID(id)
	if err != nil {
		http.Error(w, fmt.Sprintf("lookup peer: %v", err), http.StatusInternalServerError)
		return
	}
	if peer == nil {
		http.NotFound(w, r)
		return
	}

	if err := wireguard.RemovePeerByIP(peer.ClientIP); err != nil {
		http.Error(w, fmt.Sprintf("remove peer from device: %v", err), http.StatusInternalServerError)
		return
	}

	if err := db.DeleteWireguardPeer(id); err != nil {
		http.Error(w, fmt.Sprintf("delete peer: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"deleted": true,
		"peer": peerPayload{
			ID:        peer.Id,
			Name:      peer.Name,
			ClientIP:  peer.ClientIP,
			PublicKey: peer.PublicKey,
		},
	})
}

func setupWireguardAPI(r chi.Router) chi.Router {
	return r.Route("/wireguard", func(r chi.Router) {
		r.Post("/createvpn", createVPN)
		r.Post("/newPeer", newPeer)
		r.Get("/", getPeers)
		r.Delete("/{id}", deletePeer)
	})
}
