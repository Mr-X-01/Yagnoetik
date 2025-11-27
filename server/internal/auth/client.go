package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type Client struct {
	UUID      string    `json:"uuid"`
	Secret    string    `json:"secret"`
	Key       []byte    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Blocked   bool      `json:"blocked"`
	BytesUp   int64     `json:"bytes_up"`
	BytesDown int64     `json:"bytes_down"`
}

type ClientManager struct {
	clients map[string]*Client
	mutex   sync.RWMutex
}

func NewClientManager() *ClientManager {
	return &ClientManager{
		clients: make(map[string]*Client),
	}
}

func (cm *ClientManager) CreateClient(duration time.Duration) (*Client, error) {
	uuid := generateUUID()
	secret := generateSecret()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}

	client := &Client{
		UUID:      uuid,
		Secret:    secret,
		Key:       key,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(duration),
		Blocked:   false,
	}

	cm.mutex.Lock()
	cm.clients[uuid] = client
	cm.mutex.Unlock()

	return client, nil
}

func (cm *ClientManager) GetClient(uuid string) (*Client, bool) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	
	client, exists := cm.clients[uuid]
	if !exists || client.Blocked || time.Now().After(client.ExpiresAt) {
		return nil, false
	}
	
	return client, true
}

func (cm *ClientManager) DeleteClient(uuid string) bool {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	
	_, exists := cm.clients[uuid]
	if exists {
		delete(cm.clients, uuid)
	}
	return exists
}

func (cm *ClientManager) BlockClient(uuid string) bool {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	
	client, exists := cm.clients[uuid]
	if exists {
		client.Blocked = true
		return true
	}
	return false
}

func (cm *ClientManager) UnblockClient(uuid string) bool {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	
	client, exists := cm.clients[uuid]
	if exists {
		client.Blocked = false
		return true
	}
	return false
}

func (cm *ClientManager) ListClients() []*Client {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	
	clients := make([]*Client, 0, len(cm.clients))
	for _, client := range cm.clients {
		clients = append(clients, client)
	}
	return clients
}

func (cm *ClientManager) UpdateTraffic(uuid string, bytesUp, bytesDown int64) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	
	if client, exists := cm.clients[uuid]; exists {
		client.BytesUp += bytesUp
		client.BytesDown += bytesDown
	}
}

func generateUUID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return hex.EncodeToString(bytes)
}

func generateSecret() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
