package main

import (
	"Np4Protocol/go/pkg/p2p"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	dht "github.com/libp2p/go-libp2p-kad-dht"
)

func main() {
	port := flag.Int("port", 4000, "Listen port")
	flag.Parse()

	h, err := p2p.NewHost(*port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create host: %v\n", err)
		os.Exit(1)
	}
	defer h.Close()

	// Start DHT - this node becomes a DHT bootstrap peer
	ctx := context.Background()
	dhtInstance, err := dht.New(ctx, h, dht.Mode(dht.ModeServer))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create DHT: %v\n", err)
		os.Exit(1)
	}
	defer dhtInstance.Close()

	fmt.Println("Bootstrap node started")
	fmt.Printf("Peer ID: %s\n", h.ID())
	fmt.Println("Addresses:")
	for _, addr := range h.Addrs() {
		fmt.Printf("  %s/p2p/%s\n", addr, h.ID())
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	fmt.Println("\nShutting down...")
}
