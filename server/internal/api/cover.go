package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

type CoverAPI struct{}

type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
	Version   string    `json:"version"`
}

type StatusResponse struct {
	Service   string    `json:"service"`
	Uptime    string    `json:"uptime"`
	Requests  int64     `json:"requests"`
	Timestamp time.Time `json:"timestamp"`
}

var (
	startTime = time.Now()
	reqCount  int64
)

func NewCoverAPI() *CoverAPI {
	return &CoverAPI{}
}

func (c *CoverAPI) SetupRoutes() *mux.Router {
	r := mux.NewRouter()
	
	r.HandleFunc("/health", c.health).Methods("GET")
	r.HandleFunc("/api/v1/status", c.status).Methods("GET")
	r.HandleFunc("/api/v1/info", c.info).Methods("GET")
	r.HandleFunc("/", c.index).Methods("GET")
	
	r.Use(c.requestCounter)
	
	return r
}

func (c *CoverAPI) requestCounter(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		next.ServeHTTP(w, r)
	})
}

func (c *CoverAPI) health(w http.ResponseWriter, r *http.Request) {
	resp := HealthResponse{
		Status:    "ok",
		Timestamp: time.Now(),
		Version:   "1.0.0",
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (c *CoverAPI) status(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(startTime)
	
	resp := StatusResponse{
		Service:   "yagnoetik-backend",
		Uptime:    uptime.String(),
		Requests:  reqCount,
		Timestamp: time.Now(),
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (c *CoverAPI) info(w http.ResponseWriter, r *http.Request) {
	info := map[string]interface{}{
		"name":        "Yagnoetik Backend Service",
		"description": "Enterprise SaaS Backend API",
		"version":     "1.0.0",
		"environment": "production",
		"region":      "ru-central1",
		"features": []string{
			"user-management",
			"analytics",
			"real-time-processing",
		},
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func (c *CoverAPI) index(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>Yagnoetik - Enterprise Solutions</title>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; margin: 0; padding: 40px; background: #f5f5f5; }
        .container { max-width: 800px; margin: 0 auto; background: white; padding: 40px; border-radius: 8px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
        h1 { color: #333; margin-bottom: 20px; }
        .status { color: #28a745; font-weight: bold; }
        .info { background: #f8f9fa; padding: 20px; border-radius: 4px; margin: 20px 0; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Yagnoetik Enterprise Backend</h1>
        <p class="status">✓ Service Online</p>
        <div class="info">
            <h3>API Endpoints</h3>
            <ul>
                <li><code>GET /health</code> - Health check</li>
                <li><code>GET /api/v1/status</code> - Service status</li>
                <li><code>GET /api/v1/info</code> - Service information</li>
            </ul>
        </div>
        <p><small>© 2025 Yagnoetik Solutions. All rights reserved.</small></p>
    </div>
</body>
</html>`
	
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}
