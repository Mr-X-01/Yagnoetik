package main

import (
	"crypto/tls"
	"log"
	"net/http"
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
<html>
<head>
    <title>Yagnoetik VPN - Admin Panel</title>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <script src="https://unpkg.com/htmx.org@1.9.10"></script>
    <script src="https://unpkg.com/alpinejs@3.x.x/dist/cdn.min.js" defer></script>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; margin: 0; padding: 20px; background: #f5f5f5; }
        .container { max-width: 1200px; margin: 0 auto; }
        .header { background: white; padding: 20px; border-radius: 8px; margin-bottom: 20px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .card { background: white; padding: 20px; border-radius: 8px; margin-bottom: 20px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .btn { padding: 8px 16px; border: none; border-radius: 4px; cursor: pointer; margin: 2px; }
        .btn-primary { background: #007bff; color: white; }
        .btn-danger { background: #dc3545; color: white; }
        .btn-success { background: #28a745; color: white; }
        .btn-warning { background: #ffc107; color: black; }
        .form-group { margin-bottom: 15px; }
        .form-control { width: 100%; padding: 8px; border: 1px solid #ddd; border-radius: 4px; }
        table { width: 100%; border-collapse: collapse; }
        th, td { padding: 12px; text-align: left; border-bottom: 1px solid #ddd; }
        th { background: #f8f9fa; }
        .status-active { color: #28a745; }
        .status-blocked { color: #dc3545; }
        .status-expired { color: #6c757d; }
        .stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 15px; margin-bottom: 20px; }
        .stat-card { background: white; padding: 15px; border-radius: 8px; text-align: center; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .stat-number { font-size: 24px; font-weight: bold; color: #007bff; }
        .stat-label { color: #6c757d; margin-top: 5px; }
    </style>
</head>
<body>
    <div class="container" x-data="adminPanel()">
        <div class="header">
            <h1>Yagnoetik VPN - Admin Panel</h1>
            <p>Управление клиентами и мониторинг системы</p>
        </div>

        <div class="stats">
            <div class="stat-card">
                <div class="stat-number" x-text="stats.total">0</div>
                <div class="stat-label">Всего клиентов</div>
            </div>
            <div class="stat-card">
                <div class="stat-number" x-text="stats.active">0</div>
                <div class="stat-label">Активных</div>
            </div>
            <div class="stat-card">
                <div class="stat-number" x-text="stats.blocked">0</div>
                <div class="stat-label">Заблокированных</div>
            </div>
            <div class="stat-card">
                <div class="stat-number" x-text="formatBytes(stats.totalTraffic)">0</div>
                <div class="stat-label">Общий трафик</div>
            </div>
        </div>

        <div class="card">
            <h3>Создать нового клиента</h3>
            <form @submit.prevent="createClient()">
                <div class="form-group">
                    <label>Срок действия:</label>
                    <select class="form-control" x-model="newClient.duration">
                        <option value="24h">1 день</option>
                        <option value="168h">1 неделя</option>
                        <option value="720h">1 месяц</option>
                        <option value="8760h">1 год</option>
                    </select>
                </div>
                <button type="submit" class="btn btn-primary">Создать клиента</button>
            </form>
        </div>

        <div class="card">
            <h3>Список клиентов</h3>
            <button @click="loadClients()" class="btn btn-primary">Обновить</button>
            
            <table>
                <thead>
                    <tr>
                        <th>UUID</th>
                        <th>Секрет</th>
                        <th>Создан</th>
                        <th>Истекает</th>
                        <th>Статус</th>
                        <th>Трафик</th>
                        <th>Действия</th>
                    </tr>
                </thead>
                <tbody>
                    <template x-for="client in clients" :key="client.uuid">
                        <tr>
                            <td x-text="client.uuid.substring(0, 8) + '...'"></td>
                            <td x-text="client.secret.substring(0, 8) + '...'"></td>
                            <td x-text="formatDate(client.created_at)"></td>
                            <td x-text="formatDate(client.expires_at)"></td>
                            <td>
                                <span :class="getStatusClass(client)" x-text="getStatusText(client)"></span>
                            </td>
                            <td x-text="formatBytes(client.bytes_up + client.bytes_down)"></td>
                            <td>
                                <button @click="toggleBlock(client)" 
                                        :class="client.blocked ? 'btn btn-success' : 'btn btn-warning'"
                                        x-text="client.blocked ? 'Разблокировать' : 'Заблокировать'">
                                </button>
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
                stats: {
                    total: 0,
                    active: 0,
                    blocked: 0,
                    totalTraffic: 0
                },
                newClient: {
                    duration: '720h'
                },

                async init() {
                    await this.loadClients();
                },

                async loadClients() {
                    try {
                        const response = await fetch('/api/clients');
                        this.clients = await response.json();
                        this.updateStats();
                    } catch (error) {
                        alert('Ошибка загрузки клиентов: ' + error.message);
                    }
                },

                async createClient() {
                    try {
                        const response = await fetch('/api/clients', {
                            method: 'POST',
                            headers: {
                                'Content-Type': 'application/json'
                            },
                            body: JSON.stringify({
                                duration: this.newClient.duration
                            })
                        });
                        
                        if (response.ok) {
                            const result = await response.json();
                            alert('Клиент создан!\nUUID: ' + result.uuid + '\nSecret: ' + result.secret);
                            await this.loadClients();
                        } else {
                            throw new Error('Ошибка создания клиента');
                        }
                    } catch (error) {
                        alert('Ошибка: ' + error.message);
                    }
                },

                async toggleBlock(client) {
                    try {
                        const action = client.blocked ? 'unblock' : 'block';
                        const response = await fetch('/api/clients/' + client.uuid + '/' + action, {
                            method: 'POST'
                        });
                        
                        if (response.ok) {
                            await this.loadClients();
                        } else {
                            throw new Error('Ошибка изменения статуса');
                        }
                    } catch (error) {
                        alert('Ошибка: ' + error.message);
                    }
                },

                async deleteClient(uuid) {
                    if (!confirm('Удалить клиента?')) return;
                    
                    try {
                        const response = await fetch('/api/clients/' + uuid, {
                            method: 'DELETE'
                        });
                        
                        if (response.ok) {
                            await this.loadClients();
                        } else {
                            throw new Error('Ошибка удаления клиента');
                        }
                    } catch (error) {
                        alert('Ошибка: ' + error.message);
                    }
                },

                updateStats() {
                    this.stats.total = this.clients.length;
                    this.stats.active = this.clients.filter(c => !c.blocked && new Date(c.expires_at) > new Date()).length;
                    this.stats.blocked = this.clients.filter(c => c.blocked).length;
                    this.stats.totalTraffic = this.clients.reduce((sum, c) => sum + c.bytes_up + c.bytes_down, 0);
                },

                getStatusClass(client) {
                    if (client.blocked) return 'status-blocked';
                    if (new Date(client.expires_at) < new Date()) return 'status-expired';
                    return 'status-active';
                },

                getStatusText(client) {
                    if (client.blocked) return 'Заблокирован';
                    if (new Date(client.expires_at) < new Date()) return 'Истек';
                    return 'Активен';
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
	buf := make([]byte, 1024)
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
	apiURL := "https://your-server.com:8443"
	apiKey := "your-secret-api-key-here"

	panel := NewAdminPanel(apiURL, apiKey)

	r := mux.NewRouter()
	r.HandleFunc("/", panel.indexHandler).Methods("GET")
	r.PathPrefix("/api/").HandlerFunc(panel.proxyHandler)

	log.Println("Admin panel starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}
