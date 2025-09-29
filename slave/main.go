package main

import (
	"context"
	"log"
	"time"

	pb "github.com/Maruqes/512SvMan/api/proto"
	"google.golang.org/grpc"
)

func main() {
	conn, err := grpc.NewClient("localhost:50051", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Falha na conexão: %v", err)
	}
	defer conn.Close()

	client := pb.NewHelloServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := client.SayHello(ctx, &pb.HelloRequest{Name: "Marques"})
	if err != nil {
		log.Fatalf("Erro no pedido: %v", err)
	}

	log.Printf("Resposta do servidor: %s", resp.Message)
}
