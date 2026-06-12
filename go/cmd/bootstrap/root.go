package main

import (
	"github.com/spf13/cobra"
)

var port int

var rootCmd = &cobra.Command{
	Use:   "np4bootstrap",
	Short: "Np4Protocol DHT bootstrap node",
	Long:  "Np4Protocol bootstrap node - a long-lived DHT server that helps peers discover each other",
}

func init() {
	rootCmd.PersistentFlags().IntVar(&port, "port", 4000, "TCP listen port")
}
