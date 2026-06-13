package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"Np4Protocol/go/pkg/message"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/spf13/cobra"
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Enter interactive chat mode",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Peer ID: %s\n", node.ID())
		fmt.Println("Addresses:")
		for _, addr := range node.Addrs() {
			fmt.Printf("  %s\n", addr)
		}
		fmt.Println()
		fmt.Println("Commands: peers, connect <multiaddr>, send <peer-id> <msg>, id, help, quit")
		fmt.Println()

		node.OnMessage(func(msg *message.Message) {
			fmt.Printf("\n[%s] %s: %s\n> ", time.Now().Format("15:04:05"), msg.SenderID, string(msg.Content))
		})

		scanner := bufio.NewScanner(os.Stdin)
		fmt.Print("> ")
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				fmt.Print("> ")
				continue
			}

			parts := strings.SplitN(line, " ", 2)
			cmd := parts[0]

			switch cmd {
			case "quit", "exit":
				fmt.Println("Bye!")
				return nil

			case "id":
				fmt.Printf("Peer ID: %s\n", node.ID())
				for _, addr := range node.Addrs() {
					fmt.Printf("  %s\n", addr)
				}

			case "peers":
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				peerChan, err := node.FindPeers(ctx, rendezvous)
				if err != nil {
					fmt.Printf("DHT not available: %v\n", err)
					cancel()
					break
				}
				count := 0
				for pi := range peerChan {
					if pi.ID == node.ID() {
						continue
					}
					fmt.Printf("  %s  %v\n", pi.ID, pi.Addrs)
					count++
				}
				cancel()
				if count == 0 {
					fmt.Println("No peers found")
				} else {
					fmt.Printf("%d peer(s) discovered\n", count)
				}

			case "connect":
				if len(parts) < 2 {
					fmt.Println("Usage: connect <multiaddr>")
					break
				}
				maddr, err := multiaddr.NewMultiaddr(parts[1])
				if err != nil {
					fmt.Printf("Invalid multiaddr: %v\n", err)
					break
				}
				info, err := peer.AddrInfoFromP2pAddr(maddr)
				if err != nil {
					fmt.Printf("Invalid peer info: %v\n", err)
					break
				}
				if err := node.Connect(*info); err != nil {
					fmt.Printf("Connect failed: %v\n", err)
				} else {
					fmt.Printf("Connected to %s\n", info.ID)
				}

			case "send":
				if len(parts) < 2 {
					fmt.Println("Usage: send <peer-id> <message>")
					break
				}
				sendParts := strings.SplitN(parts[1], " ", 2)
				if len(sendParts) < 2 {
					fmt.Println("Usage: send <peer-id> <message>")
					break
				}
				pid, err := peer.Decode(sendParts[0])
				if err != nil {
					fmt.Printf("Invalid peer ID: %v\n", err)
					break
				}
				if err := node.Send(pid, []byte(sendParts[1])); err != nil {
					fmt.Printf("Send failed: %v\n", err)
				} else {
					fmt.Printf("Sent to %s\n", pid)
				}

			case "help":
				fmt.Println("Commands:")
				fmt.Println("  peers                    - Discover online peers via DHT")
				fmt.Println("  connect <multiaddr>      - Connect to a peer")
				fmt.Println("  send <peer-id> <message> - Send a message")
				fmt.Println("  id                       - Show this node's info")
				fmt.Println("  quit / exit              - Exit")

			default:
				fmt.Printf("Unknown command: %s (type 'help' for commands)\n", cmd)
			}
			fmt.Print("> ")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(chatCmd)
}
