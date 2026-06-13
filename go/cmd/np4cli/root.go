package main

import (
	"context"
	"fmt"
	"os"

	"Np4Protocol/go/pkg/np4"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/spf13/cobra"
)

var (
	port         int
	bootstrap    string
	rendezvous   string
	hops         int
	identityPath string
)

var rootCmd = &cobra.Command{
	Use:   "np4cli",
	Short: "Np4Protocol P2P client",
	Long:  "Np4Protocol anonymous communication client with libp2p peer discovery",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		n, err := initNode()
		if err != nil {
			return err
		}
		ctx := context.WithValue(cmd.Context(), nodeKey{}, n)
		cmd.SetContext(ctx)
		return nil
	},
}

// nodeKey is the context key used to stash the *np4.Node for subcommands.
type nodeKey struct{}

// getNode retrieves the node from the command context.
func getNode(cmd *cobra.Command) *np4.Node {
	return cmd.Context().Value(nodeKey{}).(*np4.Node)
}

// np4Node is a type alias so chat helpers don't have to import np4.
type np4Node = np4.Node

func init() {
	defaultID := os.ExpandEnv("$HOME/.np4/identity")
	rootCmd.PersistentFlags().IntVar(&port, "port", 0, "Listen port (0 = random)")
	rootCmd.PersistentFlags().StringVar(&bootstrap, "bootstrap", "", "Bootstrap multiaddr")
	rootCmd.PersistentFlags().StringVar(&rendezvous, "rendezvous", "np4-network", "DHT rendezvous")
	rootCmd.PersistentFlags().IntVar(&hops, "hops", 3, "Number of mix hops")
	rootCmd.PersistentFlags().StringVar(&identityPath, "identity", defaultID, "Persistent identity file")
}

func initNode() (*np4.Node, error) {
	opts := []np4.Option{
		np4.WithIdentity(identityPath),
		np4.WithRendezvous(rendezvous),
		np4.WithHops(hops),
	}
	if bootstrap != "" {
		maddr, err := multiaddr.NewMultiaddr(bootstrap)
		if err != nil {
			return nil, fmt.Errorf("invalid bootstrap: %w", err)
		}
		info, err := peer.AddrInfoFromP2pAddr(maddr)
		if err != nil {
			return nil, fmt.Errorf("invalid bootstrap peer info: %w", err)
		}
		opts = append(opts, np4.WithBootstrap([]peer.AddrInfo{*info}))
	}
	n, err := np4.NewNode(port, opts...)
	if err != nil {
		return nil, fmt.Errorf("create node: %w", err)
	}
	return n, nil
}
