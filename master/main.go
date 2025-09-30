package main

import (
	"512SvMan/env512"
	"512SvMan/nfs"
	"512SvMan/npm"
	"512SvMan/protocol"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	proto "github.com/Maruqes/512SvMan/api/proto/nfs"
)

func webServer() {
	http.HandleFunc("/connections", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		data, err := json.Marshal(protocol.Connections)
		if err != nil {
			http.Error(w, "failed to marshal connections", http.StatusInternalServerError)
			return
		}

		_, _ = w.Write(data)
	})

	http.HandleFunc("/click", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
			return
		}

		conn := protocol.GetConnectionByAddr("127.0.0.1")
		if conn == nil || conn.Connection == nil {
			http.Error(w, "slave 127.0.0.1 not connected", http.StatusServiceUnavailable)
			return
		}

		mount := &proto.FolderMount{
			FolderPath: "/var/512SvMan/shared",
			Source:     "127.0.0.1:/var/512SvMan/shared",
			Target:     "/mnt/data/512SvMan/shared",
		}

		if err := nfs.CreateSharedFolder(conn.Connection, mount); err != nil {
			log.Printf("CreateSharedFolder failed: %v", err)
			http.Error(w, "failed to create shared folder", http.StatusInternalServerError)
			return
		}

		if err := nfs.MountSharedFolder(conn.Connection, mount); err != nil {
			log.Printf("MountSharedFolder failed: %v", err)
			http.Error(w, "failed to mount shared folder", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	log.Println("Iniciando webserver em :9595")
	if err := http.ListenAndServe(":9595", nil); err != nil {
		log.Fatalf("webserver error: %v", err)
	}
}

func askForSudo() {
	//if current program is not sudo terminate
	if os.Geteuid() != 0 {
		fmt.Println("This program needs to be run as root.")
		os.Exit(0)
	}
}

func main() {
	askForSudo()
	env512.Setup()

	//listen and connects to gRPC
	protocol.ListenGRPC()

	hostAdmin := "127.0.0.1:81"
	base := "http://" + hostAdmin

	token, err := npm.SetupNPM(base)

	if err != nil {
		panic(err)
	}

	println("NPM setup complete, token:", token)

	webServer()

	// xml, err := virsh.CreateVMCustomCPU(
	// 	"qemu:///system",
	// 	"debian-kde",
	// 	8192, 6,
	// 	"/mnt/data/debian-live-13.1.0-amd64-kde.iso", 50, // relativo -> /var/512SvMan/qcow2/debian-kde.qcow2
	// 	"/mnt/data/debian.qcow2", // relativo -> /var/512SvMan/iso/...
	// 	"",                                 // machine (user decide; "" = auto)
	// 	"default", "0.0.0.0",
	// 	"Westmere", nil, // baseline portable
	// )
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// fmt.Println("XML gravado em:", xml)

	select {}
}
