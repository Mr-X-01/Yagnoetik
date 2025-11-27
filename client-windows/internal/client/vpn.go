package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"yagnoetik-vpn-client/internal/crypto"
	"yagnoetik-vpn-client/internal/protocol"
	"yagnoetik-vpn-client/internal/tun"
	pb "yagnoetik-vpn-client/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

type Config struct {
	ServerAddr string `json:"server_addr"`
	UUID       string `json:"uuid"`
	Secret     string `json:"secret"`
	Key        []byte `json:"key"`
}

type VPNClient struct {
	config     *Config
	conn       *grpc.ClientConn
	stream     pb.TunnelService_ConnectClient
	cipher     *crypto.Cipher
	tunIface   *tun.TunInterface
	connected  bool
	mutex      sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	bytesUp    int64
	bytesDown  int64
}

func NewVPNClient(config *Config) (*VPNClient, error) {
	cipher, err := crypto.NewCipher(config.Key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %v", err)
	}

	return &VPNClient{
		config: config,
		cipher: cipher,
	}, nil
}

func (c *VPNClient) Connect() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.connected {
		return fmt.Errorf("already connected")
	}

	// Create TUN interface
	tunIface, err := tun.CreateTunInterface("yagnoetik")
	if err != nil {
		return fmt.Errorf("failed to create TUN interface: %v", err)
	}
	c.tunIface = tunIface

	// Configure TUN interface
	if err := c.tunIface.SetIP("10.8.0.2", "255.255.255.0"); err != nil {
		c.tunIface.Close()
		return fmt.Errorf("failed to set TUN IP: %v", err)
	}

	// Add default route through VPN
	if err := c.tunIface.AddRoute("0.0.0.0/0", "10.8.0.1"); err != nil {
		log.Printf("Warning: failed to add default route: %v", err)
	}

	// Connect to gRPC server
	creds := credentials.NewTLS(&tls.Config{
		ServerName: c.config.ServerAddr,
	})

	conn, err := grpc.Dial(c.config.ServerAddr+":443", grpc.WithTransportCredentials(creds))
	if err != nil {
		c.tunIface.Close()
		return fmt.Errorf("failed to connect to server: %v", err)
	}
	c.conn = conn

	// Create tunnel client
	client := pb.NewTunnelServiceClient(conn)

	// Add authentication metadata
	ctx := metadata.AppendToOutgoingContext(context.Background(),
		"uuid", c.config.UUID,
		"secret", c.config.Secret,
	)

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start streaming connection
	stream, err := client.Connect(c.ctx)
	if err != nil {
		c.cleanup()
		return fmt.Errorf("failed to start stream: %v", err)
	}
	c.stream = stream

	c.connected = true

	// Start data transfer goroutines
	go c.handleTunToStream()
	go c.handleStreamToTun()
	go c.keepAlive()

	return nil
}

func (c *VPNClient) Disconnect() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !c.connected {
		return nil
	}

	c.connected = false
	c.cancel()
	c.cleanup()

	return nil
}

func (c *VPNClient) cleanup() {
	if c.stream != nil {
		c.stream.CloseSend()
	}
	if c.conn != nil {
		c.conn.Close()
	}
	if c.tunIface != nil {
		c.tunIface.Close()
	}
}

func (c *VPNClient) handleTunToStream() {
	buf := make([]byte, 1500)

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		// Read from TUN interface
		n, err := c.tunIface.Read(buf)
		if err != nil {
			if c.connected {
				log.Printf("TUN read error: %v", err)
			}
			return
		}

		if n > 0 {
			// Create data frame
			frame := &protocol.Frame{
				Type: protocol.FrameTypeData,
				Data: buf[:n],
			}

			err := c.sendFrame(frame)
			if err != nil {
				log.Printf("Failed to send frame: %v", err)
				return
			}

			c.bytesUp += int64(n)
		}
	}
}

func (c *VPNClient) handleStreamToTun() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		// Receive encrypted frame from server
		msg, err := c.stream.Recv()
		if err != nil {
			if err == io.EOF {
				return
			}
			if c.connected {
				log.Printf("Stream recv error: %v", err)
			}
			return
		}

		// Decrypt the frame
		decrypted, err := c.cipher.Decrypt(msg.Data)
		if err != nil {
			log.Printf("Decryption error: %v", err)
			continue
		}

		// Parse frame
		if len(decrypted) < 1 {
			continue
		}

		frameType := decrypted[0]
		frameData := decrypted[1:]

		switch frameType {
		case protocol.FrameTypeData:
			// Write to TUN interface
			_, err := c.tunIface.Write(frameData)
			if err != nil {
				log.Printf("TUN write error: %v", err)
				return
			}
			c.bytesDown += int64(len(frameData))

		case protocol.FrameTypePing:
			// Send pong response
			pongFrame := &protocol.Frame{
				Type: protocol.FrameTypePong,
				Data: frameData,
			}
			c.sendFrame(pongFrame)

		case protocol.FrameTypePong:
			// Ping response received
		}
	}
}

func (c *VPNClient) sendFrame(frame *protocol.Frame) error {
	// Serialize frame
	frameData := make([]byte, 1+len(frame.Data))
	frameData[0] = frame.Type
	copy(frameData[1:], frame.Data)

	// Encrypt frame
	encrypted, err := c.cipher.Encrypt(frameData)
	if err != nil {
		return err
	}

	// Send via gRPC stream
	msg := &pb.TunnelFrame{Data: encrypted}
	return c.stream.Send(msg)
}

func (c *VPNClient) keepAlive() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			// Send ping
			pingFrame := &protocol.Frame{
				Type: protocol.FrameTypePing,
				Data: []byte("ping"),
			}

			if err := c.sendFrame(pingFrame); err != nil {
				log.Printf("Failed to send ping: %v", err)
				return
			}
		}
	}
}

func (c *VPNClient) IsConnected() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.connected
}

func (c *VPNClient) GetStats() (int64, int64) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.bytesUp, c.bytesDown
}
