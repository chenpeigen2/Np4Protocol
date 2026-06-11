package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var peersCmd = &cobra.Command{
	Use:   "peers",
	Short: "Discover online peers via DHT",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		peerChan, err := node.FindPeers(ctx, rendezvous)
		if err != nil {
			return fmt.Errorf("DHT not available (use --bootstrap flag): %w", err)
		}

		count := 0
		for pi := range peerChan {
			if pi.ID == node.ID() {
				continue
			}
			fmt.Printf("Peer: %s\n", pi.ID)
			for _, addr := range pi.Addrs {
				fmt.Printf("  %s\n", addr)
			}
			count++
		}

		if count == 0 {
			fmt.Println("No peers found")
		} else {
			fmt.Printf("\n%d peer(s) discovered\n", count)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(peersCmd)
}
