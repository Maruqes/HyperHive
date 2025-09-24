package main

import (
	"512SvMan/npm"
	"512SvMan/virsh"
	"fmt"
	"log"
	"net/http"
	"os"
)

func webServer() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		html := `<!doctype html>
<html>
	<head>
		<meta charset="utf-8">
		<title>Teste WebServer</title>
	</head>
	<body>
		<h1>Servidor de Teste</h1>
		<form method="post" action="/click">
			<button type="submit">Clique-me</button>
		</form>
	</body>
</html>`
		_, _ = w.Write([]byte(html))
	})

	http.HandleFunc("/click", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
			return
		}
		virsh.MigrateVMs("debian-kde-nat")
		//return 200
		w.WriteHeader(http.StatusOK)
	})

	log.Println("Iniciando webserver em :9090")
	if err := http.ListenAndServe(":9090", nil); err != nil {
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
	askForSudo()
	hostAdmin := "127.0.0.1:8181"
	base := "http://" + hostAdmin


	token, err := npm.SetupNPM(base)

	if err != nil {
		panic(err)
	}

	println("NPM setup complete, token:", token)

	// proxyId, err := npm.CreateProxy(base, token, npm.Proxy{
	// 	DomainNames:           []string{"test.localhost"},
	// 	ForwardScheme:         "http",
	// 	ForwardHost:           "127.0.0.1",
	// 	ForwardPort:           8080,
	// 	CachingEnabled:        false,
	// 	BlockExploits:         true,
	// 	AllowWebsocketUpgrade: true,
	// 	AccessListID:          "0",
	// 	CertificateID:         0,
	// 	Meta:                  map[string]any{"letsencrypt_agree": false, "dns_challenge": false},
	// 	AdvancedConfig:        "",
	// 	Locations:             []any{},
	// 	Http2Support:          false,
	// 	HstsEnabled:           false,
	// 	HstsSubdomains:        false,
	// 	SslForced:             false,
	// })
	// if err != nil {
	// 	panic(err)
	// }
	proxyId := 4 // hardcoded for testing
	fmt.Println("Created proxy with ID", proxyId)

	err = npm.EditProxy(base, token, npm.Proxy{
		ID:                    proxyId,
		DomainNames:           []string{"meudeus.localhost", "test2.localhost"},
		ForwardScheme:         "http",
		ForwardHost:           "127.0.0.1",
		ForwardPort:           8080,
		CachingEnabled:        false,
		BlockExploits:         true,
		AllowWebsocketUpgrade: true,
		AccessListID:          "0",
		CertificateID:         0,
		Meta:                  map[string]any{"letsencrypt_agree": false, "dns_challenge": false},
		AdvancedConfig:        "",
		Locations:             []any{},
		Http2Support:          false,
		HstsEnabled:           false,
		HstsSubdomains:        false,
		SslForced:             false,
	})
	if err != nil {
		panic(err)
	}

	virsh.SetupVMs()

	webServer()
}
