package main

import (
	"fmt"

	"Np4Protocol/go/pkg/identity"

	"github.com/multiformats/go-multiaddr"
	"github.com/spf13/cobra"
)

var idCmd = &cobra.Command{
	Use:   "id",
	Short: "Show bootstrap node's peer ID and multiaddr (from persistent identity)",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := identity.LoadOrCreate(identityPath)
		if err != nil {
			return fmt.Errorf("load identity: %w", err)
		}
		addr, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", port))
		if err != nil {
			return fmt.Errorf("build multiaddr: %w", err)
		}
		// Append /p2p/<peerID> using multiaddr string composition.
		full, err := multiaddr.NewMultiaddr(addr.String() + "/p2p/" + id.PeerID().String())
		if err != nil {
			return fmt.Errorf("build p2p multiaddr: %w", err)
		}
		fmt.Printf("Peer ID: %s\n", id.PeerID())
		fmt.Printf("Multiaddr: %s\n", full.String())
		return nil
	},
}

func init() {
	rootCmd.AddCommand(idCmd)
}
