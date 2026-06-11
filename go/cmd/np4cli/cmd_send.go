package main

import (
	"fmt"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/spf13/cobra"
)

var sendCmd = &cobra.Command{
	Use:   "send <peer-id> <message>",
	Short: "Send a message to a peer",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		pid, err := peer.Decode(args[0])
		if err != nil {
			return fmt.Errorf("invalid peer ID: %w", err)
		}
		content := strings.Join(args[1:], " ")
		if err := node.Send(pid, []byte(content)); err != nil {
			return fmt.Errorf("send failed: %w", err)
		}
		fmt.Printf("Message sent to %s\n", pid)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(sendCmd)
}
