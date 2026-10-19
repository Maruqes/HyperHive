package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	hyperhivemcp "github.com/Maruqes/HyperHive/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "hyperhive-mcp:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	}
	flags := flag.NewFlagSet("hyperhive-mcp", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: hyperhive-mcp [serve]\n\nRuns the HyperHive stdio MCP server.\nRequired environment: HYPERHIVE_API_URL, MCP_TOKEN.\nOptional: MCP_VM_BACKUP_DIR (absolute private directory for retained VM recovery records).")
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected arguments; use hyperhive-mcp --help")
	}
	server, err := hyperhivemcp.NewServer(hyperhivemcp.Config{BaseURL: getenv("HYPERHIVE_API_URL"), MCPToken: getenv("MCP_TOKEN"), VMBackupDir: getenv("MCP_VM_BACKUP_DIR")})
	if err != nil {
		return err
	}
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}
