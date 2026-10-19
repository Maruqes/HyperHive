// Task-local inspection helper; not a supported coverage CLI. It performs
// discovery only against the local in-memory registry, never backend calls.
package main

import (
	"context"
	"encoding/json"
	"os"
	"time"

	hyperhivemcp "github.com/Maruqes/HyperHive/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
	server,err:=hyperhivemcp.NewServer(hyperhivemcp.Config{BaseURL:"http://127.0.0.1:1",MCPToken:"inventory-only"})
	if err!=nil{panic(err)}
	clientTransport,serverTransport:=mcp.NewInMemoryTransports()
	serverSession,err:=server.Connect(ctx,serverTransport,nil);if err!=nil{panic(err)};defer serverSession.Close()
	session,err:=mcp.NewClient(&mcp.Implementation{Name:"source-registry-inspection"},nil).Connect(ctx,clientTransport,nil);if err!=nil{panic(err)};defer session.Close()
	list,err:=session.ListTools(ctx,nil);if err!=nil{panic(err)}
	encoder:=json.NewEncoder(os.Stdout);encoder.SetIndent("","  ");if err:=encoder.Encode(list);err!=nil{panic(err)}
}
