package main

import (
	"Np4Protocol/go/pkg/message"
	"Np4Protocol/go/pkg/np4"
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "MixNode address")
	flag.Parse()

	node, err := np4.NewNode("127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	defer node.Stop()

	fmt.Printf("Np4Protocol CLI started\n")
	fmt.Printf("Your Node ID: %s\n", node.ID())

	node.OnMessage(func(msg *message.Message) {
		fmt.Printf("\nReceived from %s: %s\n", msg.SenderID, string(msg.Content))
	})

	_ = addr // reserved for future use: connecting to a remote MixNode

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		parts := strings.SplitN(input, " ", 2)
		if len(parts) < 2 {
			fmt.Println("Usage: <dest_id> <message>")
			continue
		}

		destID := parts[0]
		content := []byte(parts[1])

		err := node.Send(destID, content)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		}
	}
}
