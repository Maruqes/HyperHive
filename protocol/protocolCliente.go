package protocol

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

func AttachProject(val string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {

		md := metadata.Pairs("x-string", val)
		ctx = metadata.NewOutgoingContext(ctx, md)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func AttachProjectStream(val string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn,
		method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {

		md := metadata.Pairs("x-string", val)
		ctx = metadata.NewOutgoingContext(ctx, md)
		return streamer(ctx, desc, cc, method, opts...)
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

func getCaCRT(masterIp string, caPort string) ([]byte, error) {
	//ca.crt do slave esta na 50054
	url := fmt.Sprintf("http://%s:%s/ca.crt", masterIp, caPort)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get ca.crt: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get ca.crt: status code %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return body, nil
}

func GenerateClientConn(ctx context.Context, addr string, grpcPort, caPort, secret string) (conn *grpc.ClientConn, err error) {
	caCRT, err := getCaCRT(addr, caPort)
	if err != nil {
		panic("could not get CA.CRT")
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCRT) {
		panic("bad CA PEM")
	}
	creds := credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    pool,
		ServerName: addr,
	})

	unary := AttachProject(secret)
	stream := AttachProjectStream(secret)

	target := addr + grpcPort
	return grpc.DialContext(ctx, target, grpc.WithTransportCredentials(creds), grpc.WithBlock(), grpc.WithUnaryInterceptor(unary), grpc.WithStreamInterceptor(stream))
}
