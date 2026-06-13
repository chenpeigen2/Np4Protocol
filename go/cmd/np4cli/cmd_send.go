package main

import (
	"fmt"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/spf13/cobra"
)

var sendAddr string

var sendCmd = &cobra.Command{
	Use:   "send <peer-id> <message>",
	Short: "Send a message to a peer",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		pid, err := peer.Decode(args[0])
		if err != nil {
			return fmt.Errorf("invalid peer ID: %w", err)
		}

		// If --addr is provided, connect first
		if sendAddr != "" {
			maddr, err := multiaddr.NewMultiaddr(sendAddr)
			if err != nil {
				return fmt.Errorf("invalid multiaddr: %w", err)
			}
			info, err := peer.AddrInfoFromP2pAddr(maddr)
			if err != nil {
				return fmt.Errorf("invalid peer info: %w", err)
			}
			if err := node.Connect(*info); err != nil {
				return fmt.Errorf("connect failed: %w", err)
			}
		}

		content := strings.Join(args[1:], " ")
		if err := node.Send(pid, []byte(content)); err != nil {
			return fmt.Errorf("send failed: %w", err)
		}
		fmt.Printf("Message sent to %s\n", pid)
		return nil
	},
}

func init() {
	sendCmd.Flags().StringVar(&sendAddr, "addr", "", "Peer multiaddr (connect before sending)")
	rootCmd.AddCommand(sendCmd)
}
