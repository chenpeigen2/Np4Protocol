package main

import (
	"Np4Protocol/go/pkg/bootstrap"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	addr := flag.String("addr", "0.0.0.0:9090", "Listen address")
	flag.Parse()

	server, err := bootstrap.NewBootstrapServer()
	if err != nil {
		log.Fatal(err)
	}

	err = server.Start(*addr)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Bootstrap server started on %s\n", *addr)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nShutting down...")
	server.Stop()
}
