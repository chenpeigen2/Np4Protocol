package main

import (
	"fmt"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/spf13/cobra"
)

var (
	sendDirect bool
	sendAddr   string
)

var sendCmd = &cobra.Command{
	Use:   "send <peer-id> <message>",
	Short: "Send a message (through mix by default; --direct bypasses)",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		n := getNode(cmd)
		pid, err := peer.Decode(args[0])
		if err != nil {
			return fmt.Errorf("invalid peer ID: %w", err)
		}

		if sendAddr != "" {
			maddr, err := multiaddr.NewMultiaddr(sendAddr)
			if err != nil {
				return fmt.Errorf("invalid multiaddr: %w", err)
			}
			info, err := peer.AddrInfoFromP2pAddr(maddr)
			if err != nil {
				return fmt.Errorf("invalid peer info: %w", err)
			}
			if err := n.Connect(*info); err != nil {
				return fmt.Errorf("connect failed: %w", err)
			}
		}

		content := strings.Join(args[1:], " ")
		if sendDirect {
			if err := n.SendDirect(pid, []byte(content)); err != nil {
				return fmt.Errorf("send direct failed: %w", err)
			}
			fmt.Printf("Sent (direct) to %s\n", pid)
			return nil
		}
		if err := n.Send(pid, []byte(content)); err != nil {
			return fmt.Errorf("send failed: %w", err)
		}
		fmt.Printf("Sent (mix, %d hops) to %s\n", hops, pid)
		return nil
	},
}

func init() {
	sendCmd.Flags().BoolVar(&sendDirect, "direct", false, "Bypass mix (single-hop direct)")
	sendCmd.Flags().StringVar(&sendAddr, "addr", "", "Peer multiaddr (connect before sending)")
	rootCmd.AddCommand(sendCmd)
}
