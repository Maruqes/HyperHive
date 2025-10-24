package protocol

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

func AttachProject(val string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {

		md := metadata.Pairs("x-project", val)
		ctx = metadata.NewOutgoingContext(ctx, md)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// ===== TLS do cliente (com cert do cliente para mTLS) =====
func BuildClientTLS(serverCrtPath, keyPath, caCrtPath, server_name string) credentials.TransportCredentials {

	cliCert, err := tls.LoadX509KeyPair(serverCrtPath, keyPath)
	if err != nil {
		panic(fmt.Errorf("load client keypair: %w", err))
	}

	caPEM, err := os.ReadFile(caCrtPath)
	if err != nil {
		panic(fmt.Errorf("read ca.crt: %w", err))
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		panic("bad CA PEM")
	}

	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cliCert}, // apresenta o cert do cliente (mTLS)
		RootCAs:      caPool,                     // valida o servidor
		// TEM de bater com o SAN/CN do servidor
		ServerName: server_name,
	}
	return credentials.NewTLS(tlsCfg)
}
