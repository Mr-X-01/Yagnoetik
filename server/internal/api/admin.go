package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"yagnoetik-vpn/internal/auth"

	"github.com/gorilla/mux"
)

type AdminAPI struct {
	clientManager *auth.ClientManager
	apiKey        string
}

type CreateClientRequest struct {
	Duration string `json:"duration"` // e.g., "30d", "1h"
}

type CreateClientResponse struct {
	UUID      string    `json:"uuid"`
	Secret    string    `json:"secret"`
	ExpiresAt time.Time `json:"expires_at"`
}

func NewAdminAPI(clientManager *auth.ClientManager, apiKey string) *AdminAPI {
	return &AdminAPI{
		clientManager: clientManager,
		apiKey:        apiKey,
	}
}

func (a *AdminAPI) SetupRoutes() *mux.Router {
	r := mux.NewRouter()
	r.Use(a.authMiddleware)
	
	r.HandleFunc("/api/clients", a.createClient).Methods("POST")
	r.HandleFunc("/api/clients", a.listClients).Methods("GET")
	r.HandleFunc("/api/clients/{uuid}", a.deleteClient).Methods("DELETE")
	r.HandleFunc("/api/clients/{uuid}/block", a.blockClient).Methods("POST")
	r.HandleFunc("/api/clients/{uuid}/unblock", a.unblockClient).Methods("POST")
	
	return r
}

func (a *AdminAPI) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("X-API-Key")
		if apiKey != a.apiKey {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *AdminAPI) createClient(w http.ResponseWriter, r *http.Request) {
	var req CreateClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	duration, err := parseDuration(req.Duration)
	if err != nil {
		http.Error(w, "Invalid duration", http.StatusBadRequest)
		return
	}

	client, err := a.clientManager.CreateClient(duration)
	if err != nil {
		http.Error(w, "Failed to create client", http.StatusInternalServerError)
		return
	}

	resp := CreateClientResponse{
		UUID:      client.UUID,
		Secret:    client.Secret,
		ExpiresAt: client.ExpiresAt,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (a *AdminAPI) listClients(w http.ResponseWriter, r *http.Request) {
	clients := a.clientManager.ListClients()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(clients)
}

func (a *AdminAPI) deleteClient(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	uuid := vars["uuid"]

	if !a.clientManager.DeleteClient(uuid) {
		http.Error(w, "Client not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *AdminAPI) blockClient(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	uuid := vars["uuid"]

	if !a.clientManager.BlockClient(uuid) {
		http.Error(w, "Client not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (a *AdminAPI) unblockClient(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	uuid := vars["uuid"]

	if !a.clientManager.UnblockClient(uuid) {
		http.Error(w, "Client not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func parseDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		days := strings.TrimSuffix(s, "d")
		d, err := time.ParseDuration(days + "h")
		if err != nil {
			return 0, err
		}
		return d * 24, nil
	}
	return time.ParseDuration(s)
}
