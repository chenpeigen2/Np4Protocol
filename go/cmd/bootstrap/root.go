package main

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	port         int
	identityPath string
)

var rootCmd = &cobra.Command{
	Use:   "np4bootstrap",
	Short: "Np4Protocol DHT bootstrap node",
	Long:  "Np4Protocol bootstrap node - a long-lived DHT server that helps peers discover each other",
}

func init() {
	defaultPath := os.ExpandEnv("$HOME/.np4/identity")
	rootCmd.PersistentFlags().IntVar(&port, "port", 4000, "TCP listen port")
	rootCmd.PersistentFlags().StringVar(&identityPath, "identity", defaultPath, "Path to persistent identity file")
}
