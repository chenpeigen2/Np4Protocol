package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

var relayCmd = &cobra.Command{
	Use:   "relay",
	Short: "Run this node as a mix relay (advertises in DHT)",
	RunE: func(cmd *cobra.Command, args []string) error {
		n := getNode(cmd)
		if err := n.ServeRelay(); err != nil {
			return fmt.Errorf("serve relay: %w", err)
		}
		fmt.Printf("Relaying as %s\n", n.ID())
		fmt.Println("Addresses:")
		for _, addr := range n.Addrs() {
			fmt.Printf("  %s\n", addr)
		}
		fmt.Println("Press Ctrl+C to stop")

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		return nil
	},
}

func init() {
	rootCmd.AddCommand(relayCmd)
}
