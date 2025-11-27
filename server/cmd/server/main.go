package main

import (
	"crypto/tls"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"yagnoetik-vpn/internal/api"
	"yagnoetik-vpn/internal/auth"
	"yagnoetik-vpn/internal/tunnel"
	pb "yagnoetik-vpn/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func main() {
	// Initialize client manager
	clientManager := auth.NewClientManager()
	
	// Create tunnel server
	tunnelServer := tunnel.NewServer(clientManager)
	
	// Setup gRPC server
	cert, err := tls.LoadX509KeyPair("server.crt", "server.key")
	if err != nil {
		log.Fatalf("Failed to load TLS certificates: %v", err)
	}
	
	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h2"},
	})
	
	grpcServer := grpc.NewServer(grpc.Creds(creds))
	pb.RegisterTunnelServiceServer(grpcServer, tunnelServer)
	
	// Setup HTTP servers
	coverAPI := api.NewCoverAPI()
	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		log.Fatal("API_KEY environment variable is required")
	}
	adminAPI := api.NewAdminAPI(clientManager, apiKey)
	
	// Main HTTPS server (port 443) - combines gRPC and HTTP
	mainMux := http.NewServeMux()
	
	// Add cover routes
	coverRouter := coverAPI.SetupRoutes()
	mainMux.Handle("/", coverRouter)
	
	// Create combined server that handles both HTTP and gRPC
	mainServer := &http.Server{
		Addr: ":8444",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ProtoMajor == 2 && r.Header.Get("Content-Type") == "application/grpc" {
				grpcServer.ServeHTTP(w, r)
			} else {
				mainMux.ServeHTTP(w, r)
			}
		}),
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   []string{"h2", "http/1.1"},
		},
	}
	
	// Admin API server (port 8443)
	adminRouter := adminAPI.SetupRoutes()
	adminServer := &http.Server{
		Addr:    ":8443",
		Handler: adminRouter,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
		},
	}
	
	// Start servers
	go func() {
		log.Println("Starting main server on :8444")
		if err := mainServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Main server failed: %v", err)
		}
	}()
	
	go func() {
		log.Println("Starting admin server on :8443")
		if err := adminServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Admin server failed: %v", err)
		}
	}()
	
	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	
	log.Println("Shutting down servers...")
	grpcServer.GracefulStop()
	mainServer.Close()
	adminServer.Close()
}
