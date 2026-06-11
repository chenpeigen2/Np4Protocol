package main

import (
	"Np4Protocol/go/pkg/message"
	"Np4Protocol/go/pkg/np4"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	port := flag.Int("port", 4001, "Listen port")
	flag.Parse()

	node, err := np4.NewNode(*port)
	if err != nil {
		log.Fatal(err)
	}
	defer node.Stop()

	fmt.Printf("Np4Protocol Node started\n")
	fmt.Printf("Node ID: %s\n", node.ID())
	fmt.Printf("Listening on: %v\n", node.Addrs())

	node.OnMessage(func(msg *message.Message) {
		fmt.Printf("Received from %s: %s\n", msg.SenderID, string(msg.Content))
	})

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nShutting down...")
}
