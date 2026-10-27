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
	host, _, err := net.SplitHostPort(*address)
	if err != nil {
		return nil, "", fmt.Errorf("invalid -listen address: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, "", errors.New("-listen must use a loopback IP address")
	}
	if flags.NArg() == 1 {
		handler, err := server.NewWithFile(flags.Arg(0))
		return handler, *address, err
	}
	return server.New(), *address, nil
}
