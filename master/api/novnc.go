package api

import (
	"512SvMan/env512"
	"512SvMan/protocol"
	"512SvMan/services"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/Maruqes/512SvMan/logger"
	"github.com/evangwt/go-vncproxy"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/net/websocket"
)

//uses https://github.com/evangwt/go-vncproxy

var vp *vncproxy.Proxy

type guestTokenInfo struct {
	VMName    string
	ExpiresAt time.Time
}

type guestTokenStore struct {
	mu     sync.RWMutex
	tokens map[string]guestTokenInfo
}

func newGuestTokenStore() *guestTokenStore {
	return &guestTokenStore{
		tokens: make(map[string]guestTokenInfo),
	}
}

func (s *guestTokenStore) set(token string, vmName string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = guestTokenInfo{
		VMName:    vmName,
		ExpiresAt: time.Now().Add(ttl),
	}
}

func (s *guestTokenStore) get(token string) (guestTokenInfo, bool) {
	s.mu.RLock()
	info, ok := s.tokens[token]
	s.mu.RUnlock()
	if !ok {
		return guestTokenInfo{}, false
	}

	// expire eagerly to avoid long-lived access
	if time.Now().After(info.ExpiresAt) {
		s.mu.Lock()
		delete(s.tokens, token)
		s.mu.Unlock()
		return guestTokenInfo{}, false
	}

	return info, true
}

var guestTokens = newGuestTokenStore()

type novncCtxKey string

const guestVMContextKey novncCtxKey = "guest_vm"

const guestTokenTTL = 24 * time.Hour

// http://localhost:9595/novnc/vnc.html?path=/novnc/ws?token=vm1
// http://localhost:9595/novnc/vnc.html?path=/novnc/ws%3Fvm%3Dplsfunfa%26slave%3Dslave1    change plsfunfa and slave1
// https://hyperhive.maruqes.com/novnc/vnc.html?path=/novnc/ws%3Fvm%3Dlivetest
func initNoVNC() {
	vp = vncproxy.New(&vncproxy.Config{
		LogLevel: vncproxy.DebugLevel,
		TokenHandler: func(r *http.Request) (string, error) {
			// map token -> VNC backend
			// e.g., token=vm1 -> localhost:5901 (adjust as needed)
			vmName := r.URL.Query().Get("vm")
			virshService := services.VirshService{}
			vm, err := virshService.GetVmByName(vmName)
			if err != nil {
				logger.Error("novnc: failed to get VM by name")
				return "", http.ErrNoLocation
			}
			if vm == nil {
				logger.Error("novnc: VM not found or VNC not configured")
				return "", http.ErrNoLocation
			}

			if guestVM := guestVMFromContext(r); guestVM != "" && guestVM != vmName {
				logger.Warnf("novnc: guest token for %s tried to open VM %s", guestVM, vmName)
				return "", http.ErrNoLocation
			}

			if GetTokenFromContext(r) == "" && guestVMFromContext(r) == "" {
				// Should not happen because middleware guards the route
				logger.Warn("novnc: websocket request without auth context blocked")
				return "", http.ErrNoLocation
			}

			slaveHeHe := protocol.GetConnectionByMachineName(vm.MachineName)
			if slaveHeHe == nil || slaveHeHe.Connection == nil {
				logger.Error("novnc: failed to get slave what the fuck")
				return "", http.ErrNoLocation
			}
			logger.Infof("novnc: connecting to VM %s on slave %s at %s:%s", vmName, slaveHeHe.MachineName, slaveHeHe.Addr, vm.NovncPort)
			return slaveHeHe.Addr + ":" + vm.NovncPort, nil
		},
	})
}

func serveNoVNCWebSocket(w http.ResponseWriter, r *http.Request) {
	websocket.Handler(vp.ServeWS).ServeHTTP(w, r)
}

func serveNoVNC(w http.ResponseWriter, r *http.Request) {
	http.StripPrefix("/novnc", http.FileServer(http.Dir("./novnc"))).ServeHTTP(w, r)
}

func portAvailable(port int) bool {
	addr := fmt.Sprintf(":%d", port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	l.Close()
	return true
}

func handleConnection(client net.Conn, ipPort string) {
	log.Printf("New connection from %s", client.RemoteAddr())
	defer client.Close()

	// Connect to the backend (the slave)
	serverConn, err := net.DialTimeout("tcp", ipPort, 5*time.Second)
	if err != nil {
		log.Printf("failed to connect to backend %s: %v", ipPort, err)
		return
	}
	defer serverConn.Close()

	// Copy data in both directions concurrently
	go io.Copy(serverConn, client)
	io.Copy(client, serverConn)

	log.Printf("Connection closed for %s", client.RemoteAddr())
}

func streamSprite(ipPort string, listenPort int, horasAberto int) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(horasAberto)*time.Hour)
		defer cancel()

		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", listenPort))
		if err != nil {
			log.Fatalf("failed to start listener: %v", err)
		}
		defer ln.Close()

		go func() {
			<-ctx.Done()
			log.Printf("timeout: closing listener %d", listenPort)
			ln.Close()
		}()

		for {
			clientConn, err := ln.Accept()
			if err != nil {
				// if the context is done, exit
				select {
				case <-ctx.Done():
					return
				default:
					log.Printf("error in Accept(): %v", err)
					continue
				}
			}

			go handleConnection(clientConn, ipPort)
		}
	}()
}

func serveSprite(w http.ResponseWriter, r *http.Request) {
	vmName := chi.URLParam(r, "vm_name")
	if vmName == "" {
		logger.Warnf("novnc: sprite request missing vm_name from %s", r.RemoteAddr)
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	logger.Infof("novnc: sprite request received for VM %s from %s", vmName, r.RemoteAddr)

	virshService := services.VirshService{}
	vm, err := virshService.GetVmByName(vmName)
	if err != nil {
		logger.Errorf("novnc: failed to fetch VM %s: %v", vmName, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if vm == nil {
		logger.Warnf("novnc: VM %s not found", vmName)
		http.Error(w, "vm not found", http.StatusInternalServerError)
		return
	}
	if vm.SpritePort == "0" {
		logger.Warnf("novnc: VM %s sprite port not configured", vmName)
		http.Error(w, "vm sprite port is not configured", http.StatusBadRequest)
		return
	}

	listenPort := 0
	found := false
	for port := env512.SPRITE_MIN; port <= env512.SPRITE_MAX; port++ {
		if portAvailable(port) {
			listenPort = port
			found = true
			logger.Infof("novnc: selected listen port %d for VM %s sprite proxy", listenPort, vmName)
			break
		}
	}
	if !found {
		logger.Errorf("novnc: no available port between %d and %d for VM %s", env512.SPRITE_MIN, env512.SPRITE_MAX, vmName)
		http.Error(w, "no port available for the server", http.StatusInternalServerError)
		return
	}

	conn := protocol.GetConnectionByMachineName(vm.MachineName)
	if conn == nil || conn.Connection == nil {
		logger.Errorf("novnc: machine %s connection unavailable for sprite proxy", vm.MachineName)
		http.Error(w, "machine connection is not available", http.StatusInternalServerError)
		return
	}

	ipPort := conn.Addr + ":" + vm.SpritePort
	const horasAberto = 1

	logger.Infof("novnc: preparing sprite tunnel for VM %s (%s) on local port %d for %d hour(s)", vmName, ipPort, listenPort, horasAberto)

	cmd := exec.Command(
		"sudo",
		"firewall-cmd",
		"--zone=FedoraServer",
		fmt.Sprintf("--add-port=%d/tcp", listenPort),
		fmt.Sprintf("--timeout=%d", horasAberto*3600),
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		logger.Errorf("novnc: failed to open firewall port: %v, output: %s", err, string(output))
		http.Error(w, "failed to configure firewall", http.StatusInternalServerError)
		return
	} else {
		logger.Infof("novnc: firewall port %d opened for sprite tunnel", listenPort)
	}

	streamSprite(ipPort, listenPort, horasAberto)
	logger.Infof("novnc: sprite tunnel ready for VM %s on port %d", vmName, listenPort)

	config := fmt.Sprintf(`[virt-viewer]
type=spice
host=%s
port=%d
delete-this-file=1
fullscreen=0
title=HyperHive VM - %s
secure-attention=0
`, env512.MASTER_INTERNET_IP, listenPort, vmName)

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(config))
}

// novncToken->vm
func guestVMFromContext(r *http.Request) string {
	if vm, ok := r.Context().Value(guestVMContextKey).(string); ok {
		return vm
	}
	return ""
}

func checkMap(r *http.Request, requestedVM string) (bool, string) {
	// Check for guest_token cookie
	cookie, err := r.Cookie("guest_token")
	if err != nil {
		return false, ""
	}

	token := strings.TrimSpace(cookie.Value)
	if token == "" {
		return false, ""
	}

	info, exists := guestTokens.get(token)
	if !exists {
		return false, ""
	}

	// if a VM was specified in the request, ensure the token matches
	if requestedVM != "" && info.VMName != requestedVM {
		logger.Warnf("novnc: guest token for vm %s attempted to access vm %s", info.VMName, requestedVM)
		return false, ""
	}

	return true, info.VMName
}

// checks for normal auth and token for vm
func authMiddlewareNOVNC(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorized, token := isAuthorized(r)
		if authorized {
			r = SetTokenInContext(r, token)
			next.ServeHTTP(w, r)
			return
		}

		requestedVM := extractVMFromRequest(r)

		authorized, vm := checkMap(r, requestedVM)
		if authorized {
			if vm != "" {
				r = r.WithContext(context.WithValue(r.Context(), guestVMContextKey, vm))
			}
			next.ServeHTTP(w, r)
			return
		}

		applyCORSHeaders(w, r)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func extractVMFromRequest(r *http.Request) string {
	if vm := r.URL.Query().Get("vm"); vm != "" {
		return vm
	}
	if vm := chi.URLParam(r, "vm_name"); vm != "" {
		return vm
	}

	// Attempt to pull vm from encoded path query param e.g. path=/novnc/ws%3Fvm%3D<vm_name>
	if rawPath := r.URL.Query().Get("path"); rawPath != "" {
		if decoded, err := url.QueryUnescape(rawPath); err == nil {
			if u, err := url.Parse(decoded); err == nil {
				if vm := u.Query().Get("vm"); vm != "" {
					return vm
				}
			}
		}
	}

	return ""
}

func serveGuestPage(w http.ResponseWriter, r *http.Request) {
	vmName := chi.URLParam(r, "vm_name")
	html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
	<title>Password Required</title>
	<script>
		window.onload = function() {
			var password = prompt("Insere a password para a VM: %s");

			if (password === null) {
				alert("Operação cancelada.");
				return;
			}

			fetch('/guest_api/guest_vm', {
				method: 'POST',
				headers: {
					'Content-Type': 'application/json'
				},
				body: JSON.stringify({ vm_name: '%s', password: password })
			})
			.then(response => {
				if (response.ok) {
					window.location.href = '/novnc/vnc.html?path=/novnc/ws%%3Fvm%%3D%s';
				} else {
					alert('Falha ao enviar a password.');
				}
			});
		}
	</script>
</head>
<body>
</body>
</html>
`, vmName, vmName, vmName)

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}

func guestPost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		VMName   string `json:"vm_name"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	virshService := services.VirshService{}
	vm, err := virshService.GetVmByName(req.VMName)
	if err != nil || vm == nil {
		http.Error(w, "VM not found", http.StatusNotFound)
		return
	}

	if req.Password == "" || req.Password != vm.VNCPassword {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	token := uuid.New().String()
	guestTokens.set(token, req.VMName, guestTokenTTL)

	http.SetCookie(w, &http.Cookie{
		Name:     "guest_token",
		Value:    token,
		Path:     "/novnc",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(guestTokenTTL.Seconds()),
		Expires:  time.Now().Add(guestTokenTTL),
	})

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Password processed"))
}

func guestNoVNCApi(r chi.Router) chi.Router {
	return r.Route("/guest_api", func(r chi.Router) {
		r.Get("/guest_page/{vm_name}", serveGuestPage)
		r.Post("/guest_vm", guestPost)
	})
}

func setupNoVNCAPI(r chi.Router) chi.Router {
	return r.Route("/novnc", func(r chi.Router) {
		r.Use(authMiddlewareNOVNC)

		r.Get("/ws", serveNoVNCWebSocket)
		r.Get("/sprite/{vm_name}", serveSprite)
		r.Get("/*", serveNoVNC)
	})
}
