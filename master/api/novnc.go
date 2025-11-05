package api

import (
	"512SvMan/env512"
	"512SvMan/protocol"
	"512SvMan/services"
	"512SvMan/virsh"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os/exec"
	"time"

	grpcVirsh "github.com/Maruqes/512SvMan/api/proto/virsh"
	"github.com/Maruqes/512SvMan/logger"
	"github.com/evangwt/go-vncproxy"
	"github.com/go-chi/chi/v5"
	"golang.org/x/net/websocket"
)

//uses https://github.com/evangwt/go-vncproxy

var vp *vncproxy.Proxy

// http://localhost:9595/novnc/vnc.html?path=/novnc/ws?token=vm1
// http://localhost:9595/novnc/vnc.html?path=/novnc/ws%3Fvm%3Dplsfunfa%26slave%3Dslave1    change plsfunfa and slave1
func initNoVNC() {
	vp = vncproxy.New(&vncproxy.Config{
		LogLevel: vncproxy.DebugLevel,
		TokenHandler: func(r *http.Request) (string, error) {
			// map token -> VNC backend
			// e.g., token=vm1 -> localhost:5901 (adjust as needed)
			vmName := r.URL.Query().Get("vm")
			slaveName := r.URL.Query().Get("slave")
			if vmName == "" || slaveName == "" {
				logger.Error("novnc: missing vm or slave parameter")
				return "", http.ErrNoLocation
			}

			slaveMachine := protocol.GetConnectionByMachineName(slaveName)
			if slaveMachine == nil {
				logger.Error("novnc: slave machine not found")
				return "", http.ErrNoLocation
			}

			vm, err := virsh.GetVmByName(slaveMachine.Connection, &grpcVirsh.GetVmByNameRequest{Name: vmName})
			if err != nil {
				logger.Error("novnc: failed to get VM by name")
				return "", http.ErrNoLocation
			}
			if vm == nil {
				logger.Error("novnc: VM not found or VNC not configured")
				return "", http.ErrNoLocation
			}
			logger.Info("novnc: connecting to VM %s on slave %s at %s:%d", vmName, slaveName, slaveMachine.Addr, vm.NovncPort)
			return slaveMachine.Addr + ":" + vm.NovncPort, nil
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
	log.Printf("Nova ligação de %s", client.RemoteAddr())
	defer client.Close()

	// Liga ao backend (o slave)
	serverConn, err := net.DialTimeout("tcp", ipPort, 5*time.Second)
	if err != nil {
		log.Printf("erro a ligar ao backend %s: %v", ipPort, err)
		return
	}
	defer serverConn.Close()

	// Copia dados em ambos os sentidos simultaneamente
	go io.Copy(serverConn, client)
	io.Copy(client, serverConn)

	log.Printf("Ligação fechada %s", client.RemoteAddr())
}

func streamSprite(ipPort string, listenPort int, horasAberto int) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(horasAberto)*time.Hour)
		defer cancel()

		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", listenPort))
		if err != nil {
			log.Fatalf("erro a iniciar listener: %v", err)
		}
		defer ln.Close()

		go func() {
			<-ctx.Done()
			log.Printf("timeout: a fechar listener %d", listenPort)
			ln.Close()
		}()

		for {
			clientConn, err := ln.Accept()
			if err != nil {
				// se o context morreu, sai
				select {
				case <-ctx.Done():
					return
				default:
					log.Printf("erro em Accept(): %v", err)
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
		logger.Warn("novnc: sprite request missing vm_name from %s", r.RemoteAddr)
		http.Error(w, "vm_name is required", http.StatusBadRequest)
		return
	}

	logger.Info("novnc: sprite request received for VM %s from %s", vmName, r.RemoteAddr)

	virshService := services.VirshService{}
	vm, err := virshService.GetVmByName(vmName)
	if err != nil {
		logger.Error("novnc: failed to fetch VM %s: %v", vmName, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if vm == nil {
		logger.Warn("novnc: VM %s not found", vmName)
		http.Error(w, "vm not found", http.StatusInternalServerError)
		return
	}
	if vm.SpritePort == "0" {
		logger.Warn("novnc: VM %s sprite port not configured", vmName)
		http.Error(w, "vm sprite port is not configured", http.StatusBadRequest)
		return
	}

	listenPort := 0
	found := false
	for port := env512.SPRITE_MIN; port <= env512.SPRITE_MAX; port++ {
		if portAvailable(port) {
			listenPort = port
			found = true
			logger.Info("novnc: selected listen port %d for VM %s sprite proxy", listenPort, vmName)
			break
		}
	}
	if !found {
		logger.Error("novnc: no available port between %d and %d for VM %s", env512.SPRITE_MIN, env512.SPRITE_MAX, vmName)
		http.Error(w, "no port available for the server", http.StatusInternalServerError)
		return
	}

	conn := protocol.GetConnectionByMachineName(vm.MachineName)
	if conn == nil || conn.Connection == nil {
		logger.Error("novnc: machine %s connection unavailable for sprite proxy", vm.MachineName)
		http.Error(w, "machine connection is not available", http.StatusInternalServerError)
		return
	}

	ipPort := conn.Addr + ":" + vm.SpritePort
	const horasAberto = 1

	logger.Info("novnc: preparing sprite tunnel for VM %s (%s) on local port %d for %d hour(s)", vmName, ipPort, listenPort, horasAberto)

	cmd := exec.Command(
		"sudo",
		"firewall-cmd",
		"--zone=FedoraServer",
		fmt.Sprintf("--add-port=%d/tcp", listenPort),
		fmt.Sprintf("--timeout=%d", horasAberto*3600),
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		logger.Error("novnc: failed to open firewall port: %v, output: %s", err, string(output))
		http.Error(w, "failed to configure firewall", http.StatusInternalServerError)
		return
	} else {
		logger.Info("novnc: firewall port %d opened for sprite tunnel", listenPort)
	}

	streamSprite(ipPort, listenPort, horasAberto)
	logger.Info("novnc: sprite tunnel ready for VM %s on port %d", vmName, listenPort)

	config := fmt.Sprintf(`[virt-viewer]
type=spice
host=%s
port=%d
delete-this-file=0
fullscreen=0
title=HyperHive VM
secure-attention=0
`, env512.MASTER_INTERNET_IP, listenPort)

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(config))
}

func setupNoVNCAPI(r chi.Router) chi.Router {
	return r.Route("/novnc", func(r chi.Router) {
		r.Get("/ws", serveNoVNCWebSocket)
		r.Get("/sprite/{vm_name}", serveSprite)
		r.Get("/*", serveNoVNC)
	})
}
