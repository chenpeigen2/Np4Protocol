package main

import (
	"Np4Protocol/go/pkg/np4"
	"fmt"
	"os"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/spf13/cobra"
)

var (
	port        int
	bootstrap   string
	rendezvous  string
	node        *np4.Node
)

var rootCmd = &cobra.Command{
	Use:   "np4cli",
	Short: "Np4Protocol P2P client",
	Long:  "Np4Protocol anonymous communication client with libp2p peer discovery",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return initNode()
	},
}

func init() {
	rootCmd.PersistentFlags().IntVar(&port, "port", 0, "Listen port (0 = random)")
	rootCmd.PersistentFlags().StringVar(&bootstrap, "bootstrap", "", "Bootstrap node multiaddr (e.g. /ip4/127.0.0.1/tcp/4000/p2p/<id>)")
	rootCmd.PersistentFlags().StringVar(&rendezvous, "rendezvous", "np4-network", "DHT rendezvous string")
}

func initNode() error {
	if bootstrap != "" {
		maddr, err := multiaddr.NewMultiaddr(bootstrap)
		if err != nil {
			return fmt.Errorf("invalid bootstrap address: %w", err)
		}
		info, err := peer.AddrInfoFromP2pAddr(maddr)
		if err != nil {
			return fmt.Errorf("invalid bootstrap peer info: %w", err)
		}
		n, err := np4.NewNodeWithBootstrap(port, []peer.AddrInfo{*info}, rendezvous)
		if err != nil {
			return fmt.Errorf("failed to create node: %w", err)
		}
		node = n
	} else {
		n, err := np4.NewNode(port)
		if err != nil {
			return fmt.Errorf("failed to create node: %w", err)
		}
		node = n
	}
	return nil
}

func printPeerID(pid peer.ID) {
	fmt.Fprintf(os.Stderr, "%s", pid)
}
