package protocol

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

/*
SERVER

receber a string do .env e a pasta com certs
retornar:
	grpc.Creds(buildServerTLS()),
	grpc.UnaryInterceptor(RequireProject(expectedProject)),


	receber a string do .env e a pasta com certs
CLIENTE:
	grpc.WithTransportCredentials(buildClientTLS()),
	grpc.WithUnaryInterceptor(AttachProject(project)),
*/

func RequireAuthUnary(expected string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}
		v := md.Get("x-string")
		if len(v) == 0 || v[0] != expected {
			return nil, status.Error(codes.Unauthenticated, "invalid x-string")
		}
		return handler(ctx, req)
	}
}

func RequireAuthStream(expected string) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		md, ok := metadata.FromIncomingContext(ss.Context())
		if !ok {
			return status.Error(codes.Unauthenticated, "missing metadata")
		}
		v := md.Get("x-string")
		if len(v) == 0 || v[0] != expected {
			return status.Error(codes.Unauthenticated, "invalid x-string")
		}
		return handler(srv, ss)
	}
}

func BuildServerTLS(serverCrtPath, keyPath, caCrtPath, expected string) credentials.TransportCredentials {
	// caminhos dos ficheiros (ajusta como quiseres ou lê de env)

	cert, err := tls.LoadX509KeyPair(serverCrtPath, keyPath)
	if err != nil {
		log.Fatalf("load server keypair: %v", err)
	}

	caPEM, err := os.ReadFile(caCrtPath)
	if err != nil {
		log.Fatalf("read ca.crt: %v", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		log.Fatal("bad CA PEM")
	}

	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    caPool,

		// Hook ANTES da conexão completar (podes enfiar regras tuas aqui)
		VerifyPeerCertificate: func(rawCerts [][]byte, chains [][]*x509.Certificate) error {
			if len(chains) == 0 || len(chains[0]) == 0 {
				return errors.New("sem cadeia verificada")
			}
			client := chains[0][0]

			ok := false
			for _, ou := range client.Subject.OrganizationalUnit {
				if ou == expected {
					ok = true
					break
				}
			}
			if !ok {
				return fmt.Errorf("cliente não autorizado (OU!=%s)", expected)
			}
			return nil
		},
	}
	return credentials.NewTLS(tlsCfg)
}

func ServeCaCRTFile(path string, master bool) {
	go func() {
		port := 50053 //for master
		if !master {
			port = 50054 // for slave
		}
		http.HandleFunc("/ca.crt", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-pem-file")
			w.Header().Set("Content-Disposition", "attachment; filename=ca.crt")
			http.ServeFile(w, r, path)
		})

		addr := fmt.Sprintf(":%d", port)
		log.Printf("Serving CA certificate at http://localhost:%d/ca.crt", port)
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()
}

func GenerateGRPCServer(isMaster bool) *grpc.Server {
	unaryServer := RequireAuthUnary("ola")
	streamServer := RequireAuthStream("ola")
	credentials := BuildServerTLS("certs/server.crt", "certs/server.key", "certs/ca.crt", "ola")
	ServeCaCRTFile("certs/ca.crt", isMaster)

	return grpc.NewServer(
		grpc.UnaryInterceptor(unaryServer),
		grpc.StreamInterceptor(streamServer),
		grpc.Creds(credentials),
	)
}
