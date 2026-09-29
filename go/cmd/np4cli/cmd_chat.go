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

var chatInsecure bool

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Enter interactive chat mode",
	RunE: func(cmd *cobra.Command, args []string) error {
		n := getNode(cmd)
		fmt.Printf("Peer ID: %s\n", n.ID())
		fmt.Println("Addresses:")
		for _, addr := range n.Addrs() {
			fmt.Printf("  %s\n", addr)
		}
		fmt.Println()
		if chatInsecure {
			fmt.Println("MODE: INSECURE direct sends (no anonymity).")
		} else {
			fmt.Println("MODE: mix routing (onion + batch shuffle).")
			fmt.Println("NOTE: peers must be online and have published keys; offline = message lost.")
			// Make ourselves addressable: without this, no peer can build
			// the final onion layer to us and the chat simply cannot receive.
			if n.DHT() == nil {
				fmt.Println("WARNING: no --bootstrap configured; mix sends will hard-fail. Use --insecure or provide --bootstrap.")
			} else if err := n.PublishKeys(); err != nil {
				fmt.Printf("WARNING: key publication failed (%v); peers may not reach you.\n", err)
			} else {
				fmt.Printf("Keys published — reachable via mix at %s\n", n.ID())
			}
		}
		fmt.Println()
		fmt.Println("Commands: peers, send <peer-id> <msg>, id, help, quit")
		if chatInsecure {
			fmt.Println("          (connect <multiaddr> is only needed in --insecure mode)")
		}
		fmt.Println()

		n.OnMessage(func(msg *message.Message) {
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
			switch parts[0] {
			case "quit", "exit":
				fmt.Println("Bye!")
				return nil
			case "id":
				fmt.Printf("Peer ID: %s\n", n.ID())
				for _, addr := range n.Addrs() {
					fmt.Printf("  %s\n", addr)
				}
			case "peers":
				runChatPeers(n)
			case "connect":
				runChatConnect(n, parts)
			case "send":
				runChatSend(n, parts)
			case "help":
				printChatHelp()
			default:
				fmt.Printf("Unknown command: %s (type 'help')\n", parts[0])
			}
			fmt.Print("> ")
		}
		return nil
	},
}

func runChatPeers(n *np4Node) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	peerChan, err := n.FindPeers(ctx, rendezvous)
	if err != nil {
		fmt.Printf("DHT not available: %v\n", err)
		return
	}
	count := 0
	for pi := range peerChan {
		if pi.ID == n.ID() {
			continue
		}
		fmt.Printf("  %s  %v\n", pi.ID, pi.Addrs)
		count++
	}
	if count == 0 {
		fmt.Println("No peers found")
	} else {
		fmt.Printf("%d peer(s) discovered\n", count)
	}
}

func runChatConnect(n *np4Node, parts []string) {
	if len(parts) < 2 {
		fmt.Println("Usage: connect <multiaddr>")
		return
	}
	maddr, err := multiaddr.NewMultiaddr(parts[1])
	if err != nil {
		fmt.Printf("Invalid multiaddr: %v\n", err)
		return
	}
	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		fmt.Printf("Invalid peer info: %v\n", err)
		return
	}
	if err := n.Connect(*info); err != nil {
		fmt.Printf("Connect failed: %v\n", err)
		return
	}
	fmt.Printf("Connected to %s\n", info.ID)
}

func runChatSend(n *np4Node, parts []string) {
	if len(parts) < 2 {
		fmt.Println("Usage: send <peer-id> <message>")
		return
	}
	sendParts := strings.SplitN(parts[1], " ", 2)
	if len(sendParts) < 2 {
		fmt.Println("Usage: send <peer-id> <message>")
		return
	}
	pid, err := peer.Decode(sendParts[0])
	if err != nil {
		fmt.Printf("Invalid peer ID: %v\n", err)
		return
	}
	if chatInsecure {
		if err := n.SendDirect(pid, []byte(sendParts[1])); err != nil {
			fmt.Printf("Send failed: %v\n", err)
			return
		}
		fmt.Printf("Sent to %s (WARNING: unprotected direct send, no anonymity)\n", pid)
		return
	}
	if err := n.Send(pid, []byte(sendParts[1])); err != nil {
		fmt.Printf("Send failed: %v\n", err)
		return
	}
	fmt.Printf("Sent (mix) to %s\n", pid)
}

func printChatHelp() {
	fmt.Println("Commands:")
	fmt.Println("  peers                    - Discover online peers via DHT")
	fmt.Println("  connect <multiaddr>      - Connect to a peer")
	fmt.Println("  send <peer-id> <message> - Send a message")
	fmt.Println("  id                       - Show this node's info")
	fmt.Println("  quit / exit              - Exit")
}

func init() {
	chatCmd.Flags().BoolVar(&chatInsecure, "insecure", false, "Use direct sends instead of mix routing (NO anonymity)")
	rootCmd.AddCommand(chatCmd)
}
