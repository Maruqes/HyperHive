package main

import (
	"512SvMan/db"
	"512SvMan/nfs"
	"512SvMan/npm"
	"512SvMan/protocol"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	proto "github.com/Maruqes/512SvMan/api/proto/nfs"
	logger "github.com/Maruqes/512SvMan/logger"
)

type sharePoint struct {
	MachineName string `json:"machine_name"` //this machine want to share
	FolderPath  string `json:"folder_path"`  //this folder
}

func getFolderName(path string) string {
	path = strings.TrimSuffix(path, "/")

	//split by /
	parts := []rune(path)
	name := ""
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == '/' {
			break
		}
		name = string(parts[i]) + name
	}
	return name
}

func nfsEndpoints() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
			return
		}
		// serve the proxy playground by default
		http.ServeFile(w, r, "web/testProxys.html")
	})

	http.HandleFunc("/connections", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		data, err := json.Marshal(protocol.GetConnectionsSnapshot())
		if err != nil {
			http.Error(w, "failed to marshal connections", http.StatusInternalServerError)
			return
		}

		_, _ = w.Write(data)
	})

	http.HandleFunc("/createSharePoint", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
			return
		}
		//get machine name
		//folder to share
		//mount on ip:/var/512SvMan/shared/folder_name
		///mnt/512SvMan/shared/folder_name

		//get from request body
		var sP sharePoint
		if err := json.NewDecoder(r.Body).Decode(&sP); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		//find connection by machine name
		conn := protocol.GetConnectionByMachineName(sP.MachineName)
		if conn == nil || conn.Connection == nil {
			http.Error(w, "slave not connected", http.StatusServiceUnavailable)
			return
		}

		mount := &proto.FolderMount{
			MachineName: sP.MachineName,
			FolderPath:  sP.FolderPath,
			Source:      conn.Addr + ":" + sP.FolderPath,
			Target:      "/mnt/512SvMan/shared/" + sP.MachineName + "_" + getFolderName(sP.FolderPath),
		}

		if err := nfs.CreateSharedFolder(conn.Connection, mount); err != nil {
			logger.Error("CreateSharedFolder failed: %v", err)
			http.Error(w, "failed to create shared folder "+err.Error(), http.StatusInternalServerError)
			return
		}

		err := db.AddNFSShare(mount.MachineName, mount.FolderPath, mount.Source, mount.Target)
		if err != nil {
			logger.Error("AddNFSShare failed: %v", err)
			http.Error(w, "failed to add NFS share to database", http.StatusInternalServerError)
			return
		}

		err = nfs.MountAllSharedFolders(protocol.GetAllGRPCConnections(), protocol.GetAllMachineNames())
		if err != nil {
			logger.Error("MountAllSharedFolders failed: %v", err)
			http.Error(w, "failed to mount all shared folders", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/removeSharePoint", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
			return
		}

		var sP sharePoint
		if err := json.NewDecoder(r.Body).Decode(&sP); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		conn := protocol.GetConnectionByMachineName(sP.MachineName)
		if conn == nil || conn.Connection == nil {
			http.Error(w, "slave not connected", http.StatusServiceUnavailable)
			return
		}

		//remove last slash

		mount := &proto.FolderMount{
			MachineName: sP.MachineName,
			FolderPath:  sP.FolderPath,
			Source:      conn.Addr + ":" + sP.FolderPath,
			Target:      "/mnt/512SvMan/shared/" + sP.MachineName + "_" + getFolderName(sP.FolderPath),
		}

		if err := nfs.RemoveSharedFolder(conn.Connection, mount); err != nil {
			logger.Error("RemoveSharedFolder failed: %v", err)
			http.Error(w, "failed to remove shared folder "+err.Error(), http.StatusInternalServerError)
			return
		}

		err := db.RemoveNFSShare(mount.MachineName, mount.FolderPath)
		if err != nil {
			logger.Error("RemoveNFSShare failed: %v", err)
			http.Error(w, "failed to remove NFS share from database", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/listShares", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
			return
		}

		shares, err := nfs.GetAllSharedFolders()
		if err != nil {
			http.Error(w, "failed to get shared folders", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		data, err := json.Marshal(shares)
		if err != nil {
			http.Error(w, "failed to marshal shares", http.StatusInternalServerError)
			return
		}

		_, _ = w.Write(data)
	})
}

func proxyEndpoints() {
	http.HandleFunc("/listProxies", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
			return
		}

		proxies, err := npm.GetAllProxys(baseURL, loginToken)
		if err != nil {
			http.Error(w, "failed to get proxies: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		data, err := json.Marshal(proxies)
		if err != nil {
			http.Error(w, "failed to marshal proxies", http.StatusInternalServerError)
			return
		}

		_, _ = w.Write(data)
	})

	http.HandleFunc("/createProxy", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
			return
		}

		var p npm.Proxy
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		_, err := npm.CreateProxy(baseURL, loginToken, p)
		if err != nil {
			http.Error(w, "failed to create proxy: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/editProxy", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
			return
		}

		var p npm.Proxy
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		err := npm.EditProxy(baseURL, loginToken, p)
		if err != nil {
			http.Error(w, "failed to edit proxy: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/deleteProxy", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
			return
		}

		var p struct {
			ID int `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		err := npm.DeleteProxy(baseURL, loginToken, p.ID)
		if err != nil {
			http.Error(w, "failed to delete proxy: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/disableProxy", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
			return
		}

		var p struct {
			ID int `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		err := npm.DisableProxy(baseURL, loginToken, p.ID)
		if err != nil {
			http.Error(w, "failed to disable proxy: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/enableProxy", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
			return
		}

		var p struct {
			ID int `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		err := npm.EnableProxy(baseURL, loginToken, p.ID)
		if err != nil {
			http.Error(w, "failed to enable proxy: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	})
}

func webServer() {
	nfsEndpoints()
	proxyEndpoints()

	http.Handle("/web/", http.StripPrefix("/web/", http.FileServer(http.Dir("./web"))))

	log.Println("Iniciando webserver em :9595")
	if err := http.ListenAndServe(":9595", nil); err != nil {
		log.Fatalf("webserver error: %v", err)
	}
}
