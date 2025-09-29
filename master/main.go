package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	pb "github.com/Maruqes/512SvMan/api/proto/hello"
	"google.golang.org/grpc"
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

		//return 200
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

// === Servidor do MASTER (HelloService) ===
type helloServer struct {
	pb.UnimplementedHelloServiceServer
}

func (s *helloServer) SayHello(ctx context.Context, req *pb.HelloRequest) (*pb.HelloResponse, error) {
	log.Printf("Master recebeu: %s", req.GetName())
	return &pb.HelloResponse{Message: "Olá, " + req.GetName()}, nil
}

func (s *helloServer) SetConnection(ctx context.Context, req *pb.SetConnectionRequest) (*pb.SetConnectionResponse, error) {
	log.Printf("Master recebeu SetConnection: %s:%d", req.GetHost(), req.GetPort())
	return &pb.SetConnectionResponse{Ok: "OK do Master"}, nil
}

func listenGRPC() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterHelloServiceServer(s, &helloServer{})
	go func() {
		log.Println("Master a ouvir em :50051")
		if err := s.Serve(lis); err != nil {
			log.Fatalf("serve: %v", err)
		}
	}()
}

func main() {
	askForSudo()
	askForSudo()

	// // 2) (Exemplo) Master chama o CLIENTE (ClientService) em :50052
	// time.Sleep(300 * time.Millisecond) // só para garantir que o cliente já subiu
	// conn, err := grpc.Dial("localhost:50052", grpc.WithTransportCredentials(insecure.NewCredentials()))
	// if err != nil {
	// 	log.Fatalf("dial cliente: %v", err)
	// }
	// defer conn.Close()
	// c := pb.NewClientServiceClient(conn)

	// ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	// defer cancel()
	// resp, err := c.Notify(ctx, &pb.NotifyRequest{Text: "Ping do Master"})
	// if err != nil {
	// 	log.Fatalf("Notify: %v", err)
	// }
	// log.Printf("Resposta do cliente: %s", resp.GetOk())

	// hostAdmin := "127.0.0.1:81"
	// base := "http://" + hostAdmin

	// token, err := npm.SetupNPM(base)

	// if err != nil {
	// 	panic(err)
	// }

	// println("NPM setup complete, token:", token)

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
	// proxyId := 4 // hardcoded for testing
	// fmt.Println("Created proxy with ID", proxyId)

	// err = npm.EditProxy(base, token, npm.Proxy{
	// 	ID:                    proxyId,
	// 	DomainNames:           []string{"meudeus.localhost", "test2.localhost"},
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

	// webServer()

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
}
