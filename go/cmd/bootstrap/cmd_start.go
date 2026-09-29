package main

import (
	"context"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"Np4Protocol/go/pkg/np4"
	"Np4Protocol/go/pkg/pathsel"

	"github.com/gin-gonic/gin"
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
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		// The bootstrap is a full np4 node in DHT-server mode: it seeds the
		// DHT AND relays onion traffic. Every client holds an outbound
		// connection to it, so the last onion hop to a NAT'd client rides an
		// already-established connection instead of an impossible inbound
		// dial. Single-relay deployments pair with client --hops 1.
		node, err := np4.NewNode(port,
			np4.WithIdentity(identityPath),
			np4.WithDHTServer(),
		)
		if err != nil {
			return fmt.Errorf("bootstrap node: %w", err)
		}
		defer node.Close()

		startTime = time.Now()

		fmt.Println("Bootstrap node started (DHT seed + mix relay)")
		fmt.Printf("Peer ID: %s\n", node.ID())
		fmt.Println("Addresses:")
		for _, addr := range node.Addrs() {
			fmt.Printf("  %s\n", addr)
		}
		fmt.Println()
		fmt.Println("On a public server the printed listen IP is the internal one;")
		fmt.Println("hand clients the multiaddr with the PUBLIC IP, e.g.:")
		fmt.Printf("  np4cli --bootstrap %s --hops 1 chat\n", node.Addrs()[0])
		fmt.Println()

		if webPort > 0 {
			go startGinServer(node)
			fmt.Printf("Dashboard: http://localhost:%d\n", webPort)
		}

		// Advertise as mix relay. Publishing the key needs a non-empty DHT
		// routing table (records are stored on peers), and on a fresh
		// bootstrap only the first joining client provides one — retry until
		// then. An in-flight wait returns as soon as a peer appears, so the
		// relay becomes routable within ~seconds of the first client joining.
		fmt.Println("[bootstrap] relay advertisement starts once the first client joins the DHT")
		go func() {
			for {
				if err := node.ServeRelay(); err == nil {
					fmt.Println("[bootstrap] advertised as mix relay (np4-relay rendezvous)")
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(3 * time.Second):
				}
			}
		}()

		<-ctx.Done()
		fmt.Println("\nShutting down...")
		return nil
	},
}

func startGinServer(node *np4.Node) {
	h := node.Host()
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
		addrs := node.Addrs()
		rtSize := 0
		if dht := node.DHT(); dht != nil {
			rtSize = dht.RoutingTable().Size()
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

		finder := &pathsel.DHTFinder{DHT: node.DHT(), Timeout: 5 * time.Second}
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
