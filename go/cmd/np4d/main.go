package main

import (
	"Np4Protocol/go/pkg/np4"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	addr := flag.String("addr", "0.0.0.0:8080", "Listen address")
	flag.Parse()

	node, err := np4.NewNode(*addr)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Np4Protocol MixNode started on %s\n", node.Addr())
	fmt.Printf("Node ID: %s\n", node.ID())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nShutting down...")
	node.Stop()
}
