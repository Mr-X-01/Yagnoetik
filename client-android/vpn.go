package main

import (
	"context"
	"crypto/cipher"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

// VPNService provides the main VPN functionality for Android
type VPNService struct {
	config     *Config
	conn       *grpc.ClientConn
	stream     TunnelService_ConnectClient
	cipher     *Cipher
	connected  bool
	mutex      sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	bytesUp    int64
	bytesDown  int64
	tunFd      int
}

type Config struct {
	ServerAddr string `json:"server_addr"`
	UUID       string `json:"uuid"`
	Secret     string `json:"secret"`
	Key        []byte `json:"key"`
}

type Cipher struct {
	aead cipher.AEAD
}

// Protocol frame types
const (
	FrameTypeData = 0
	FrameTypePing = 1
	FrameTypePong = 2
)

// NewVPNService creates a new VPN service instance
func NewVPNService() *VPNService {
	return &VPNService{}
}

// LoadConfig loads configuration from JSON string
func (v *VPNService) LoadConfig(configJSON string) error {
	var config Config
	err := json.Unmarshal([]byte(configJSON), &config)
	if err != nil {
		return fmt.Errorf("failed to parse config: %v", err)
	}

	v.config = &config

	// Create cipher
	cipher, err := NewCipher(config.Key)
	if err != nil {
		return fmt.Errorf("failed to create cipher: %v", err)
	}
	v.cipher = cipher

	return nil
}

// Connect establishes VPN connection
func (v *VPNService) Connect(tunFd int) error {
	v.mutex.Lock()
	defer v.mutex.Unlock()

	if v.connected {
		return fmt.Errorf("already connected")
	}

	v.tunFd = tunFd

	// Connect to gRPC server
	creds := credentials.NewTLS(&tls.Config{
		ServerName: v.config.ServerAddr,
	})

	conn, err := grpc.Dial(v.config.ServerAddr+":443", grpc.WithTransportCredentials(creds))
	if err != nil {
		return fmt.Errorf("failed to connect to server: %v", err)
	}
	v.conn = conn

	// Create tunnel client
	client := NewTunnelServiceClient(conn)

	// Add authentication metadata
	ctx := metadata.AppendToOutgoingContext(context.Background(),
		"uuid", v.config.UUID,
		"secret", v.config.Secret,
	)

	v.ctx, v.cancel = context.WithCancel(ctx)

	// Start streaming connection
	stream, err := client.Connect(v.ctx)
	if err != nil {
		v.cleanup()
		return fmt.Errorf("failed to start stream: %v", err)
	}
	v.stream = stream

	v.connected = true

	// Start data transfer goroutines
	go v.handleTunToStream()
	go v.handleStreamToTun()
	go v.keepAlive()

	return nil
}

// Disconnect closes VPN connection
func (v *VPNService) Disconnect() error {
	v.mutex.Lock()
	defer v.mutex.Unlock()

	if !v.connected {
		return nil
	}

	v.connected = false
	v.cancel()
	v.cleanup()

	return nil
}

func (v *VPNService) cleanup() {
	if v.stream != nil {
		v.stream.CloseSend()
	}
	if v.conn != nil {
		v.conn.Close()
	}
}

func (v *VPNService) handleTunToStream() {
	buf := make([]byte, 1500)
	tunConn := &TunConn{fd: v.tunFd}

	for {
		select {
		case <-v.ctx.Done():
			return
		default:
		}

		// Read from TUN interface
		n, err := tunConn.Read(buf)
		if err != nil {
			if v.connected {
				log.Printf("TUN read error: %v", err)
			}
			return
		}

		if n > 0 {
			err := v.sendFrame(FrameTypeData, buf[:n])
			if err != nil {
				log.Printf("Failed to send frame: %v", err)
				return
			}

			v.bytesUp += int64(n)
		}
	}
}

func (v *VPNService) handleStreamToTun() {
	tunConn := &TunConn{fd: v.tunFd}

	for {
		select {
		case <-v.ctx.Done():
			return
		default:
		}

		// Receive encrypted frame from server
		msg, err := v.stream.Recv()
		if err != nil {
			if err == io.EOF {
				return
			}
			if v.connected {
				log.Printf("Stream recv error: %v", err)
			}
			return
		}

		// Decrypt the frame
		decrypted, err := v.cipher.Decrypt(msg.Data)
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
		case FrameTypeData:
			// Write to TUN interface
			_, err := tunConn.Write(frameData)
			if err != nil {
				log.Printf("TUN write error: %v", err)
				return
			}
			v.bytesDown += int64(len(frameData))

		case FrameTypePing:
			// Send pong response
			v.sendFrame(FrameTypePong, frameData)

		case FrameTypePong:
			// Ping response received
		}
	}
}

func (v *VPNService) sendFrame(frameType byte, data []byte) error {
	// Serialize frame
	frameData := make([]byte, 1+len(data))
	frameData[0] = frameType
	copy(frameData[1:], data)

	// Encrypt frame
	encrypted, err := v.cipher.Encrypt(frameData)
	if err != nil {
		return err
	}

	// Send via gRPC stream
	msg := &TunnelFrame{Data: encrypted}
	return v.stream.Send(msg)
}

func (v *VPNService) keepAlive() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-v.ctx.Done():
			return
		case <-ticker.C:
			if err := v.sendFrame(FrameTypePing, []byte("ping")); err != nil {
				log.Printf("Failed to send ping: %v", err)
				return
			}
		}
	}
}

// IsConnected returns connection status
func (v *VPNService) IsConnected() bool {
	v.mutex.RLock()
	defer v.mutex.RUnlock()
	return v.connected
}

// GetStats returns traffic statistics
func (v *VPNService) GetStats() (int64, int64) {
	v.mutex.RLock()
	defer v.mutex.RUnlock()
	return v.bytesUp, v.bytesDown
}

// TunConn wraps file descriptor for TUN interface
type TunConn struct {
	fd int
}

func (t *TunConn) Read(buf []byte) (int, error) {
	// This would use syscall.Read in actual implementation
	// For now, return mock data
	return 0, fmt.Errorf("not implemented")
}

func (t *TunConn) Write(buf []byte) (int, error) {
	// This would use syscall.Write in actual implementation
	// For now, return mock data
	return len(buf), nil
}

// Cipher implementation
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes")
	}

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}

	return &Cipher{aead: aead}, nil
}

func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	// In real implementation, use crypto/rand
	ciphertext := c.aead.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

func (c *Cipher) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < c.aead.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce := ciphertext[:c.aead.NonceSize()]
	encrypted := ciphertext[c.aead.NonceSize():]

	plaintext, err := c.aead.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}

// gRPC types (simplified for mobile)
type TunnelFrame struct {
	Data []byte
}

type TunnelService_ConnectClient interface {
	Send(*TunnelFrame) error
	Recv() (*TunnelFrame, error)
	CloseSend() error
}

type TunnelServiceClient interface {
	Connect(ctx context.Context, opts ...grpc.CallOption) (TunnelService_ConnectClient, error)
}

func NewTunnelServiceClient(conn *grpc.ClientConn) TunnelServiceClient {
	// This would return actual gRPC client in real implementation
	return nil
}

// Export for gomobile
var vpnService = NewVPNService()

func LoadConfig(configJSON string) string {
	err := vpnService.LoadConfig(configJSON)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return "OK"
}

func Connect(tunFd int) string {
	err := vpnService.Connect(tunFd)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return "OK"
}

func Disconnect() string {
	err := vpnService.Disconnect()
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return "OK"
}

func IsConnected() bool {
	return vpnService.IsConnected()
}

func GetBytesUp() int64 {
	up, _ := vpnService.GetStats()
	return up
}

func GetBytesDown() int64 {
	_, down := vpnService.GetStats()
	return down
}

func main() {
	// Required for gomobile
}
