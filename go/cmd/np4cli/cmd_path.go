package main

import (
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/spf13/cobra"
)

var pathCmd = &cobra.Command{
	Use:   "path <peer-id>",
	Short: "Show the mix path that would be used to reach a peer",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		n := getNode(cmd)
		dest, err := peer.Decode(args[0])
		if err != nil {
			return fmt.Errorf("invalid peer ID: %w", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		path, err := n.PickPath(ctx, dest)
		if err != nil {
			return fmt.Errorf("pick path: %w", err)
		}
		fmt.Printf("Path (%d hops + dest):\n", len(path))
		for i, hop := range path {
			fmt.Printf("  [%d] %s\n", i+1, hop)
		}
		fmt.Printf("  [dest] %s\n", dest)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(pathCmd)
}
