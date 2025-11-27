#!/bin/bash

# Yagnoetik VPN - Конфигурация сервера для production
# Автоматически настраивает сервер с правильными портами и SSL

set -e

DOMAIN=${1:-localhost}
API_KEY=${2:-$(openssl rand -hex 32)}

echo "🔧 Настройка production конфигурации..."

# Обновляем конфигурацию сервера для работы с реальными сертификатами
cat > /opt/yagnoetik/Yagnoetik/server/cmd/server/main.go << 'EOF'
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
	// Получаем конфигурацию из переменных окружения
	domain := os.Getenv("DOMAIN")
	if domain == "" {
		domain = "localhost"
	}
	
	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		log.Fatal("API_KEY environment variable is required")
	}
	
	tlsCert := os.Getenv("TLS_CERT")
	tlsKey := os.Getenv("TLS_KEY")
	
	if tlsCert == "" || tlsKey == "" {
		log.Fatal("TLS_CERT and TLS_KEY environment variables are required")
	}

	// Создаем менеджер клиентов
	clientManager := auth.NewClientManager()
	
	// Создаем gRPC сервер с TLS
	cert, err := tls.LoadX509KeyPair(tlsCert, tlsKey)
	if err != nil {
		log.Fatalf("Failed to load TLS certificates: %v", err)
	}
	
	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		ServerName:   domain,
	})
	
	grpcServer := grpc.NewServer(grpc.Creds(creds))
	tunnelServer := tunnel.NewServer(clientManager)
	pb.RegisterTunnelServiceServer(grpcServer, tunnelServer)
	
	// Создаем HTTP сервер для маскировочных эндпоинтов
	coverAPI := api.NewCoverAPI()
	mainMux := http.NewMux()
	coverAPI.RegisterRoutes(mainMux)
	
	// Основной сервер на порту 8444 (за Nginx)
	mainServer := &http.Server{
		Addr:    ":8444",
		Handler: mainMux,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
		},
	}
	
	// Админ API сервер на порту 8443 (HTTP для внутреннего использования)
	adminAPI := api.NewAdminAPI(clientManager, apiKey)
	adminMux := http.NewMux()
	adminAPI.RegisterRoutes(adminMux)
	
	adminServer := &http.Server{
		Addr:    ":8443",
		Handler: adminMux,
	}
	
	// Запуск серверов
	go func() {
		log.Printf("Starting main server on :8444 (domain: %s)", domain)
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
	
	// Ожидание сигнала завершения
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	
	log.Println("Shutting down servers...")
	grpcServer.GracefulStop()
	mainServer.Close()
	adminServer.Close()
}
EOF

# Обновляем конфигурацию админ-панели
cat > /opt/yagnoetik/Yagnoetik/admin-panel/main.go << 'EOF'
package main

import (
	"crypto/tls"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
)

type Client struct {
	UUID      string    `json:"uuid"`
	Secret    string    `json:"secret"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Blocked   bool      `json:"blocked"`
	BytesUp   int64     `json:"bytes_up"`
	BytesDown int64     `json:"bytes_down"`
}

type AdminPanel struct {
	apiURL string
	apiKey string
}

func NewAdminPanel(apiURL, apiKey string) *AdminPanel {
	return &AdminPanel{
		apiURL: apiURL,
		apiKey: apiKey,
	}
}

func (a *AdminPanel) indexHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := `<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Yagnoetik VPN - Админ панель</title>
    <script src="https://unpkg.com/htmx.org@1.9.10"></script>
    <script src="https://unpkg.com/alpinejs@3.x.x/dist/cdn.min.js" defer></script>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; background: #f5f5f5; }
        .container { max-width: 1200px; margin: 0 auto; background: white; padding: 20px; border-radius: 8px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
        .header { border-bottom: 2px solid #007bff; padding-bottom: 10px; margin-bottom: 20px; }
        .btn { padding: 8px 16px; margin: 4px; border: none; border-radius: 4px; cursor: pointer; }
        .btn-primary { background: #007bff; color: white; }
        .btn-danger { background: #dc3545; color: white; }
        .btn-success { background: #28a745; color: white; }
        .btn-warning { background: #ffc107; color: black; }
        .form-group { margin-bottom: 15px; }
        .form-control { width: 100%; padding: 8px; border: 1px solid #ddd; border-radius: 4px; }
        .table { width: 100%; border-collapse: collapse; margin-top: 20px; }
        .table th, .table td { padding: 12px; text-align: left; border-bottom: 1px solid #ddd; }
        .table th { background: #f8f9fa; font-weight: bold; }
        .status-active { color: #28a745; font-weight: bold; }
        .status-blocked { color: #dc3545; font-weight: bold; }
        .client-form { background: #f8f9fa; padding: 20px; border-radius: 8px; margin-bottom: 20px; }
        .error { color: #dc3545; margin: 10px 0; }
        .success { color: #28a745; margin: 10px 0; }
    </style>
</head>
<body>
    <div class="container" x-data="adminPanel()">
        <div class="header">
            <h1>🔒 Yagnoetik VPN - Админ панель</h1>
            <p>Управление VPN клиентами</p>
        </div>

        <!-- Форма создания клиента -->
        <div class="client-form">
            <h3>➕ Создать нового клиента</h3>
            <form @submit.prevent="createClient()">
                <div class="form-group">
                    <label>Срок действия (дни):</label>
                    <input type="number" x-model="newClient.days" class="form-control" value="30" min="1" max="365">
                </div>
                <button type="submit" class="btn btn-success">Создать клиента</button>
            </form>
            <div x-show="error" class="error" x-text="error"></div>
            <div x-show="success" class="success" x-text="success"></div>
        </div>

        <!-- Список клиентов -->
        <div>
            <h3>👥 Список клиентов</h3>
            <button @click="loadClients()" class="btn btn-primary">🔄 Обновить</button>
            
            <table class="table">
                <thead>
                    <tr>
                        <th>UUID</th>
                        <th>Секрет</th>
                        <th>Создан</th>
                        <th>Истекает</th>
                        <th>Статус</th>
                        <th>Трафик ↑</th>
                        <th>Трафик ↓</th>
                        <th>Действия</th>
                    </tr>
                </thead>
                <tbody>
                    <template x-for="client in clients" :key="client.uuid">
                        <tr>
                            <td><code x-text="client.uuid.substring(0,8)"></code></td>
                            <td><code x-text="client.secret.substring(0,8)"></code></td>
                            <td x-text="formatDate(client.created_at)"></td>
                            <td x-text="formatDate(client.expires_at)"></td>
                            <td>
                                <span :class="client.blocked ? 'status-blocked' : 'status-active'" 
                                      x-text="client.blocked ? '🚫 Заблокирован' : '✅ Активен'"></span>
                            </td>
                            <td x-text="formatBytes(client.bytes_up)"></td>
                            <td x-text="formatBytes(client.bytes_down)"></td>
                            <td>
                                <button @click="toggleBlock(client)" 
                                        :class="client.blocked ? 'btn btn-success' : 'btn btn-warning'"
                                        x-text="client.blocked ? 'Разблокировать' : 'Заблокировать'"></button>
                                <button @click="deleteClient(client.uuid)" class="btn btn-danger">Удалить</button>
                            </td>
                        </tr>
                    </template>
                </tbody>
            </table>
        </div>
    </div>

    <script>
        function adminPanel() {
            return {
                clients: [],
                newClient: { days: 30 },
                error: '',
                success: '',

                async loadClients() {
                    try {
                        const response = await fetch('/api/clients');
                        if (!response.ok) throw new Error('Ошибка загрузки');
                        this.clients = await response.json();
                        this.error = '';
                    } catch (e) {
                        this.error = 'Ошибка загрузки клиентов: ' + e.message;
                    }
                },

                async createClient() {
                    try {
                        const response = await fetch('/api/clients', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ duration: this.newClient.days + 'd' })
                        });
                        
                        if (!response.ok) throw new Error('Ошибка создания');
                        
                        this.success = 'Клиент успешно создан!';
                        this.error = '';
                        this.loadClients();
                        
                        setTimeout(() => this.success = '', 3000);
                    } catch (e) {
                        this.error = 'Ошибка создания клиента: ' + e.message;
                        this.success = '';
                    }
                },

                async toggleBlock(client) {
                    try {
                        const action = client.blocked ? 'unblock' : 'block';
                        const response = await fetch(`/api/clients/${client.uuid}/${action}`, {
                            method: 'POST'
                        });
                        
                        if (!response.ok) throw new Error('Ошибка операции');
                        
                        this.loadClients();
                        this.error = '';
                    } catch (e) {
                        this.error = 'Ошибка: ' + e.message;
                    }
                },

                async deleteClient(uuid) {
                    if (!confirm('Удалить клиента?')) return;
                    
                    try {
                        const response = await fetch(`/api/clients/${uuid}`, {
                            method: 'DELETE'
                        });
                        
                        if (!response.ok) throw new Error('Ошибка удаления');
                        
                        this.loadClients();
                        this.error = '';
                    } catch (e) {
                        this.error = 'Ошибка удаления: ' + e.message;
                    }
                },

                formatDate(dateStr) {
                    return new Date(dateStr).toLocaleString('ru-RU');
                },

                formatBytes(bytes) {
                    if (bytes === 0) return '0 B';
                    const k = 1024;
                    const sizes = ['B', 'KB', 'MB', 'GB'];
                    const i = Math.floor(Math.log(bytes) / Math.log(k));
                    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
                },

                init() {
                    this.loadClients();
                }
            }
        }
    </script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(tmpl))
}

func (a *AdminPanel) proxyHandler(w http.ResponseWriter, r *http.Request) {
	// Proxy requests to the main server API
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: tr,
	}
	
	req, err := http.NewRequest(r.Method, a.apiURL+r.URL.Path, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Copy headers
	for key, values := range r.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	// Add API key
	req.Header.Set("X-API-Key", a.apiKey)

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(resp.StatusCode)
	
	// Copy response body
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
}

func main() {
	serverURL := os.Getenv("SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8443"
	}
	
	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		log.Fatal("API_KEY environment variable is required")
	}
	
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	adminPanel := NewAdminPanel(serverURL, apiKey)
	
	r := mux.NewRouter()
	r.HandleFunc("/", adminPanel.indexHandler)
	r.PathPrefix("/api/").HandlerFunc(adminPanel.proxyHandler)
	
	log.Printf("Admin panel starting on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
EOF

echo "✅ Production конфигурация обновлена!"
echo "🔧 Основной сервер будет работать на порту 8444 за Nginx"
echo "🔧 Админ API на порту 8443 (HTTP для внутреннего использования)"
echo "🔧 Админ-панель на порту 8081 за Nginx"
