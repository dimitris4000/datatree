package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"datatree/internal/document"
	"datatree/internal/server"
)

func main() {
	handler, address, err := application(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		log.Fatal(err)
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	fmt.Printf("Data Tree: http://%s\n", listener.Addr())
	if err = srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func application(args []string) (*server.Server, string, error) {
	flags := flag.NewFlagSet("datatree", flag.ContinueOnError)
	address := flags.String("listen", "127.0.0.1:8080", "loopback listen address")
	maxFileMiB := flags.Int64("max-file-size-mb", int64(document.MaxBytes/(1024*1024)), "maximum document/comparison size in MiB")
	maxNodes := flags.Int("max-nodes", document.MaxNodes, "maximum parsed nodes per document, including mapping keys")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: datatree [flags] [file.json|file.jsonl|file.yaml|file.yml|file.csv]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return nil, "", err
	}
	if flags.NArg() > 1 {
		return nil, "", errors.New("expected at most one file argument; usage: datatree [flags] [file]")
	}
	if *maxFileMiB <= 0 || *maxFileMiB > int64(int(^uint(0)>>1))/(1024*1024) || *maxNodes <= 0 {
		return nil, "", errors.New("-max-file-size-mb and -max-nodes must be positive and within supported ranges")
	}
	limits := document.Limits{MaxBytes: int(*maxFileMiB * 1024 * 1024), MaxNodes: *maxNodes}
	if err := limits.Validate(); err != nil {
		return nil, "", err
	}
	host, _, err := net.SplitHostPort(*address)
	if err != nil {
		return nil, "", fmt.Errorf("invalid -listen address: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, "", errors.New("-listen must use a loopback IP address")
	}
	if flags.NArg() == 1 {
		handler, err := server.NewWithFile(flags.Arg(0), limits)
		return handler, *address, err
	}
	return server.New(limits), *address, nil
}
