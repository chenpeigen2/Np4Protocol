package main

import (
	"Np4Protocol/go/pkg/message"
	"Np4Protocol/go/pkg/np4"
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: np4cli <listen-port>")
		os.Exit(1)
	}

	port := 0
	if n, _ := fmt.Sscanf(os.Args[1], "%d", &port); n != 1 {
		fmt.Fprintf(os.Stderr, "invalid port: %s\n", os.Args[1])
		os.Exit(1)
	}

	node, err := np4.NewNode(port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create node: %v\n", err)
		os.Exit(1)
	}
	defer node.Stop()

	fmt.Printf("Node ID: %s\n", node.ID())
	fmt.Printf("Addresses: %v\n", node.Addrs())

	node.OnMessage(func(msg *message.Message) {
		fmt.Printf("\n[%s]: %s\n> ", msg.SenderID, string(msg.Content))
	})

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			fmt.Print("> ")
			continue
		}

		if strings.HasPrefix(line, "connect ") {
			addr := strings.TrimPrefix(line, "connect ")
			maddr, err := multiaddr.NewMultiaddr(addr)
			if err != nil {
				fmt.Printf("invalid address: %v\n", err)
				fmt.Print("> ")
				continue
			}
			info, err := peer.AddrInfoFromP2pAddr(maddr)
			if err != nil {
				fmt.Printf("invalid peer info: %v\n", err)
				fmt.Print("> ")
				continue
			}
			if err := node.Connect(info.Addrs, info.ID); err != nil {
				fmt.Printf("connect failed: %v\n", err)
			} else {
				fmt.Printf("connected to %s\n", info.ID)
			}
		} else if strings.HasPrefix(line, "send ") {
			parts := strings.SplitN(strings.TrimPrefix(line, "send "), " ", 2)
			if len(parts) != 2 {
				fmt.Println("Usage: send <peer-id> <message>")
				fmt.Print("> ")
				continue
			}
			pid, err := peer.Decode(parts[0])
			if err != nil {
				fmt.Printf("invalid peer ID: %v\n", err)
				fmt.Print("> ")
				continue
			}
			if err := node.Send(pid, []byte(parts[1])); err != nil {
				fmt.Printf("send failed: %v\n", err)
			}
		} else {
			fmt.Println("Commands: connect <multiaddr>, send <peer-id> <message>")
		}
		fmt.Print("> ")
	}
}
