package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var idCmd = &cobra.Command{
	Use:   "id",
	Short: "Show this node's peer ID and addresses",
	Run: func(cmd *cobra.Command, args []string) {
		n := getNode(cmd)
		fmt.Printf("Peer ID: %s\n", n.ID())
		fmt.Println("Addresses:")
		for _, addr := range n.Addrs() {
			fmt.Printf("  %s\n", addr)
		}
	},
}

func init() {
	rootCmd.AddCommand(idCmd)
}
