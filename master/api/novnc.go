package api

import (
	"512SvMan/env512"
	"512SvMan/protocol"
	"512SvMan/services"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/Maruqes/512SvMan/logger"
	"github.com/evangwt/go-vncproxy"
	"github.com/go-chi/chi/v5"
	"golang.org/x/net/websocket"
)

//uses https://github.com/evangwt/go-vncproxy

var vp *vncproxy.Proxy

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

func streamSprite(ipPort string, ln net.Listener, listenPort int, horasAberto int) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(horasAberto)*time.Hour)
		defer cancel()
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

	conn := protocol.GetConnectionByMachineName(vm.MachineName)
	if conn == nil || conn.Connection == nil {
		logger.Errorf("novnc: machine %s connection unavailable for sprite proxy", vm.MachineName)
		http.Error(w, "machine connection is not available", http.StatusInternalServerError)
		return
	}

	listenPort := 0
	var ln net.Listener
	found := false
	for port := env512.SPRITE_MIN; port <= env512.SPRITE_MAX; port++ {
		candidate, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			continue
		}
		listenPort = port
		ln = candidate
		found = true
		logger.Infof("novnc: selected listen port %d for VM %s sprite proxy", listenPort, vmName)
		break
	}
	if !found {
		logger.Errorf("novnc: no available port between %d and %d for VM %s", env512.SPRITE_MIN, env512.SPRITE_MAX, vmName)
		http.Error(w, "no port available for the server", http.StatusInternalServerError)
		return
	}

	ipPort := conn.Addr + ":" + vm.SpritePort
	const horasAberto = 1

	logger.Infof("novnc: preparing sprite tunnel for VM %s (%s) on local port %d for %d hour(s)", vmName, ipPort, listenPort, horasAberto)

	streamSprite(ipPort, ln, listenPort, horasAberto)
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

// checks for normal auth
func authMiddlewareNOVNC(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorized, token := isAuthorized(r)
		if authorized {
			r = SetTokenInContext(r, token)
			next.ServeHTTP(w, r)
			return
		}

		applyCORSHeaders(w, r)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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
