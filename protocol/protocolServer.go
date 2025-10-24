package protocol

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
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

func RequireAuth(expected string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{},
		info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {

		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "sem metadata")
		}
		v := md.Get("x-string")
		if len(v) == 0 || v[0] != expected {
			return nil, status.Error(codes.Unauthenticated, "string invalida")
		}
		return handler(ctx, req)
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
