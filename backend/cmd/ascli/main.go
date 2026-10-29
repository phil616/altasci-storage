package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/altasci/network-storage/backend/internal/ascli"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := ascli.Run(ctx, os.Args[1:], os.Stdout, os.Getenv, version)
	stop()
	os.Exit(code)
}
