package tunnel

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"yagnoetik-vpn/internal/auth"
	"yagnoetik-vpn/internal/crypto"
	"yagnoetik-vpn/internal/protocol"
	pb "yagnoetik-vpn/proto"

	"google.golang.org/grpc/metadata"
)

type Server struct {
	pb.UnimplementedTunnelServiceServer
	clientManager *auth.ClientManager
	connections   map[string]*Connection
	connMutex     sync.RWMutex
}

type Connection struct {
	client     *auth.Client
	cipher     *crypto.Cipher
	stream     pb.TunnelService_ConnectServer
	tunConn    net.Conn
	lastPing   time.Time
	bytesUp    int64
	bytesDown  int64
	ctx        context.Context
	cancel     context.CancelFunc
}

func NewServer(clientManager *auth.ClientManager) *Server {
	return &Server{
		clientManager: clientManager,
		connections:   make(map[string]*Connection),
	}
}

func (s *Server) Connect(stream pb.TunnelService_ConnectServer) error {
	// Authenticate client from metadata
	ctx := stream.Context()
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return fmt.Errorf("no metadata")
	}

	uuidValues := md.Get("uuid")
	secretValues := md.Get("secret")
	
	if len(uuidValues) == 0 || len(secretValues) == 0 {
		return fmt.Errorf("missing credentials")
	}

	uuid := uuidValues[0]
	secret := secretValues[0]

	client, exists := s.clientManager.GetClient(uuid)
	if !exists || client.Secret != secret {
		return fmt.Errorf("invalid credentials")
	}

	// Create cipher for this connection
	cipher, err := crypto.NewCipher(client.Key)
	if err != nil {
		return fmt.Errorf("failed to create cipher: %v", err)
	}

	// Create TUN connection
	tunConn, err := s.createTunConnection()
	if err != nil {
		return fmt.Errorf("failed to create tun connection: %v", err)
	}
	defer tunConn.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	conn := &Connection{
		client:    client,
		cipher:    cipher,
		stream:    stream,
		tunConn:   tunConn,
		lastPing:  time.Now(),
		ctx:       ctx,
		cancel:    cancel,
	}

	s.connMutex.Lock()
	s.connections[uuid] = conn
	s.connMutex.Unlock()

	defer func() {
		s.connMutex.Lock()
		delete(s.connections, uuid)
		s.connMutex.Unlock()
		
		// Update traffic stats
		s.clientManager.UpdateTraffic(uuid, conn.bytesUp, conn.bytesDown)
	}()

	// Start goroutines for data transfer
	errChan := make(chan error, 2)
	
	go s.handleStreamToTun(conn, errChan)
	go s.handleTunToStream(conn, errChan)
	go s.keepAlive(conn)

	// Wait for error or context cancellation
	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) handleStreamToTun(conn *Connection, errChan chan error) {
	for {
		select {
		case <-conn.ctx.Done():
			errChan <- conn.ctx.Err()
			return
		default:
		}

		// Receive encrypted frame from client
		msg, err := conn.stream.Recv()
		if err != nil {
			if err == io.EOF {
				errChan <- nil
				return
			}
			errChan <- fmt.Errorf("stream recv error: %v", err)
			return
		}

		// Decrypt the frame
		decrypted, err := conn.cipher.Decrypt(msg.Data)
		if err != nil {
			log.Printf("Decryption error: %v", err)
			continue
		}

		// Parse frame
		frame := &protocol.Frame{}
		if len(decrypted) < 1 {
			continue
		}
		frame.Type = decrypted[0]
		frame.Data = decrypted[1:]

		switch frame.Type {
		case protocol.FrameTypeData:
			// Write to TUN interface
			_, err := conn.tunConn.Write(frame.Data)
			if err != nil {
				errChan <- fmt.Errorf("tun write error: %v", err)
				return
			}
			conn.bytesDown += int64(len(frame.Data))

		case protocol.FrameTypePing:
			// Send pong response
			pongFrame := &protocol.Frame{
				Type: protocol.FrameTypePong,
				Data: frame.Data,
			}
			s.sendFrame(conn, pongFrame)
			conn.lastPing = time.Now()

		case protocol.FrameTypePong:
			conn.lastPing = time.Now()
		}
	}
}

func (s *Server) handleTunToStream(conn *Connection, errChan chan error) {
	buf := make([]byte, 1500) // MTU size
	
	for {
		select {
		case <-conn.ctx.Done():
			errChan <- conn.ctx.Err()
			return
		default:
		}

		// Read from TUN interface
		n, err := conn.tunConn.Read(buf)
		if err != nil {
			errChan <- fmt.Errorf("tun read error: %v", err)
			return
		}

		if n > 0 {
			// Create data frame
			frame := &protocol.Frame{
				Type: protocol.FrameTypeData,
				Data: buf[:n],
			}

			err := s.sendFrame(conn, frame)
			if err != nil {
				errChan <- fmt.Errorf("send frame error: %v", err)
				return
			}
			
			conn.bytesUp += int64(n)
		}
	}
}

func (s *Server) sendFrame(conn *Connection, frame *protocol.Frame) error {
	// Serialize frame
	frameData := make([]byte, 1+len(frame.Data))
	frameData[0] = frame.Type
	copy(frameData[1:], frame.Data)

	// Encrypt frame
	encrypted, err := conn.cipher.Encrypt(frameData)
	if err != nil {
		return err
	}

	// Send via gRPC stream
	msg := &pb.TunnelFrame{Data: encrypted}
	return conn.stream.Send(msg)
}

func (s *Server) keepAlive(conn *Connection) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-conn.ctx.Done():
			return
		case <-ticker.C:
			// Check if connection is alive
			if time.Since(conn.lastPing) > 30*time.Second {
				log.Printf("Connection timeout for client %s", conn.client.UUID)
				conn.cancel()
				return
			}

			// Send ping
			pingFrame := &protocol.Frame{
				Type: protocol.FrameTypePing,
				Data: []byte("ping"),
			}
			
			if err := s.sendFrame(conn, pingFrame); err != nil {
				log.Printf("Failed to send ping: %v", err)
				conn.cancel()
				return
			}
		}
	}
}

func (s *Server) createTunConnection() (net.Conn, error) {
	// This is a placeholder - in real implementation, this would create
	// a connection to the TUN interface or routing system
	// For now, we'll create a simple TCP connection to a local service
	return net.Dial("tcp", "127.0.0.1:8080")
}
