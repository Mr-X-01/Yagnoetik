package main

import (
	"encoding/json"
	"log"
	"os"

	"yagnoetik-vpn-client/internal/client"
	"yagnoetik-vpn-client/internal/ui"
)

func main() {
	// Load configuration
	config, err := loadConfig("config.json")
	if err != nil {
		log.Printf("Failed to load config: %v, using defaults", err)
		config = &client.Config{
			ServerAddr: "your-server.com",
			UUID:       "default-uuid",
			Secret:     "default-secret",
			Key:        make([]byte, 32),
		}
	}

	// Create VPN client
	vpnClient, err := client.NewVPNClient(config)
	if err != nil {
		log.Fatalf("Failed to create VPN client: %v", err)
	}

	// Create and run GUI
	gui := ui.NewGUI(vpnClient)
	if err := gui.Run(); err != nil {
		log.Fatalf("GUI error: %v", err)
	}
}

func loadConfig(filename string) (*client.Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var config client.Config
	err = json.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}
