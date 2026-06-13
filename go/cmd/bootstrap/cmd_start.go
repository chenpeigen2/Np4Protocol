package main

import (
	"Np4Protocol/go/pkg/identity"
	"Np4Protocol/go/pkg/p2p"
	"Np4Protocol/go/pkg/pathsel"
	"context"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/spf13/cobra"
)

//go:embed web/*
var webFiles embed.FS

var webPort int

var startTime time.Time

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the bootstrap node",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := identity.LoadOrCreate(identityPath)
		if err != nil {
			return fmt.Errorf("load identity: %w", err)
		}
		h, err := p2p.NewHostWithIdentity(id, port)
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

		startTime = time.Now()

		fmt.Println("Bootstrap node started")
		fmt.Printf("Peer ID: %s\n", h.ID())
		fmt.Println("Addresses:")
		for _, addr := range h.Addrs() {
			fmt.Printf("  %s/p2p/%s\n", addr, h.ID())
		}

		if webPort > 0 {
			go startGinServer(h, dhtInstance)
			fmt.Printf("Dashboard: http://localhost:%d\n", webPort)
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

func startGinServer(h host.Host, dhtInstance *dht.IpfsDHT) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Next()
	})

	// Serve embedded static files
	webFS, _ := fs.Sub(webFiles, "web")
	r.StaticFS("/static", http.FS(webFS))

	r.GET("/", func(c *gin.Context) {
		data, _ := webFiles.ReadFile("web/index.html")
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	})

	r.GET("/api/status", func(c *gin.Context) {
		addrs := make([]string, len(h.Addrs()))
		for i, addr := range h.Addrs() {
			addrs[i] = addr.String() + "/p2p/" + h.ID().String()
		}
		rtSize := 0
		if dhtInstance != nil {
			rtSize = dhtInstance.RoutingTable().Size()
		}
		c.JSON(http.StatusOK, gin.H{
			"peer_id":   h.ID().String(),
			"addresses": addrs,
			"uptime":    time.Since(startTime).Round(time.Second).String(),
			"dht_peers": rtSize,
			"status":    "online",
		})
	})

	r.GET("/api/peers", func(c *gin.Context) {
		peers := h.Peerstore().Peers()
		peerList := make([]gin.H, 0, len(peers))
		for _, pid := range peers {
			if pid == h.ID() {
				continue
			}
			addrs := h.Peerstore().Addrs(pid)
			addrStrs := make([]string, len(addrs))
			for i, addr := range addrs {
				addrStrs[i] = addr.String()
			}
			peerList = append(peerList, gin.H{
				"id":        pid.String(),
				"addresses": addrStrs,
			})
		}

		c.JSON(http.StatusOK, peerList)
	})

	r.GET("/api/relays", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		finder := &pathsel.DHTFinder{DHT: dhtInstance, Timeout: 5 * time.Second}
		relays, err := finder.FindRelays(ctx)
		if err != nil {
			c.JSON(http.StatusOK, []interface{}{}) // empty on error
			return
		}
		out := make([]gin.H, 0, len(relays))
		for _, r := range relays {
			out = append(out, gin.H{
				"id":       r.ID.String(),
				"ecdh_pub": hex.EncodeToString(r.ECDHPub),
			})
		}
		c.JSON(http.StatusOK, out)
	})

	r.Run(fmt.Sprintf(":%d", webPort))
}

func init() {
	startCmd.Flags().IntVar(&webPort, "web", 8080, "Web dashboard port (0 to disable)")
	rootCmd.AddCommand(startCmd)
}
