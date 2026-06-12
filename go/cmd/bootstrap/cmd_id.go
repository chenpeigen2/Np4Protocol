package main

import (
	"Np4Protocol/go/pkg/p2p"
	"fmt"

	"github.com/spf13/cobra"
)

var idCmd = &cobra.Command{
	Use:   "id",
	Short: "Show bootstrap node's peer ID and multiaddr",
	RunE: func(cmd *cobra.Command, args []string) error {
		h, err := p2p.NewHost(port)
		if err != nil {
			return fmt.Errorf("failed to create host: %w", err)
		}
		defer h.Close()

		fmt.Printf("Peer ID: %s\n", h.ID())
		fmt.Println("Addresses:")
		for _, addr := range h.Addrs() {
			fmt.Printf("  %s/p2p/%s\n", addr, h.ID())
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(idCmd)
}
