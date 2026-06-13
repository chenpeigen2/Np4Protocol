package main

import (
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/spf13/cobra"
)

var connectCmd = &cobra.Command{
	Use:   "connect <multiaddr>",
	Short: "Connect to a peer",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		n := getNode(cmd)
		maddr, err := multiaddr.NewMultiaddr(args[0])
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
		fmt.Printf("Connected to %s\n", info.ID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(connectCmd)
}
