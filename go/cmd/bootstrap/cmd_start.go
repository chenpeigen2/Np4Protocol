package main

import (
	"Np4Protocol/go/pkg/p2p"
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the bootstrap node",
	RunE: func(cmd *cobra.Command, args []string) error {
		h, err := p2p.NewHost(port)
		if err != nil {
			return fmt.Errorf("failed to create host: %w", err)
		}
		defer h.Close()

		ctx := context.Background()
		dhtInstance, err := dht.New(ctx, h, dht.Mode(dht.ModeServer))
		if err != nil {
			return fmt.Errorf("failed to create DHT: %w", err)
		}
		defer dhtInstance.Close()

		fmt.Println("Bootstrap node started")
		fmt.Printf("Peer ID: %s\n", h.ID())
		fmt.Println("Addresses:")
		for _, addr := range h.Addrs() {
			fmt.Printf("  %s/p2p/%s\n", addr, h.ID())
		}
		fmt.Println()
		fmt.Println("Use the multiaddr above with np4cli --bootstrap flag")
		fmt.Println("Press Ctrl+C to stop")

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		fmt.Println("\nShutting down...")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}
