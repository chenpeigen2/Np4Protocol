package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var idCmd = &cobra.Command{
	Use:   "id",
	Short: "Show this node's peer ID and addresses",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Peer ID: %s\n", node.ID())
		fmt.Println("Addresses:")
		for _, addr := range node.Addrs() {
			fmt.Printf("  %s\n", addr)
		}
	},
}

func init() {
	rootCmd.AddCommand(idCmd)
}
