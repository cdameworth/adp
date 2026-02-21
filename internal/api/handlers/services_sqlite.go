package handlers

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Service represents a service record for the in-memory SQLite-mode store.
type Service struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SQLiteServiceHandler handles service-related HTTP requests using an
// in-memory map. This is the SQLite-mode counterpart to ServiceHandler
// (which requires a PostgreSQL-backed database.ServiceStore).
type SQLiteServiceHandler struct {
	mu       sync.RWMutex
	services map[string]*Service
}

// NewSQLiteServiceHandler creates a new in-memory service handler for SQLite mode.
func NewSQLiteServiceHandler() *SQLiteServiceHandler {
	return &SQLiteServiceHandler{
		services: make(map[string]*Service),
	}
}

// CreateService handles POST /v1/services
func (h *SQLiteServiceHandler) CreateService(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := parseJSON(r, &req); err != nil {
		writeBadRequest(w, "Invalid request body: "+err.Error())
		return
	}

	if req.Name == "" {
		writeBadRequest(w, "name is required")
		return
	}

	now := time.Now()
	svc := &Service{
		ID:          "svc_" + uuid.New().String()[:8],
		Name:        req.Name,
		Description: req.Description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	h.mu.Lock()
	h.services[svc.ID] = svc
	h.mu.Unlock()

	writeCreated(w, svc)
}

// GetService handles GET /v1/services/{id}
func (h *SQLiteServiceHandler) GetService(w http.ResponseWriter, r *http.Request) {
	id := getPathParam(r, "id")
	if id == "" {
		writeBadRequest(w, "Service ID is required")
		return
	}

	h.mu.RLock()
	svc, ok := h.services[id]
	h.mu.RUnlock()

	if !ok {
		writeNotFound(w, "Service not found")
		return
	}

	writeSuccess(w, svc)
}

// ListServices handles GET /v1/services
func (h *SQLiteServiceHandler) ListServices(w http.ResponseWriter, r *http.Request) {
	limit := getQueryParamInt(r, "limit", 50)
	offset := getQueryParamInt(r, "offset", 0)
	nameFilter := getQueryParam(r, "name", "")

	h.mu.RLock()
	var all []*Service
	for _, svc := range h.services {
		if nameFilter != "" && !strings.Contains(strings.ToLower(svc.Name), strings.ToLower(nameFilter)) {
			continue
		}
		all = append(all, svc)
	}
	h.mu.RUnlock()

	total := len(all)

	// Apply offset and limit.
	if offset > len(all) {
		all = nil
	} else {
		all = all[offset:]
		if limit > 0 && limit < len(all) {
			all = all[:limit]
		}
	}

	writeList(w, all, total, limit, offset)
}

// UpdateService handles PATCH /v1/services/{id}
func (h *SQLiteServiceHandler) UpdateService(w http.ResponseWriter, r *http.Request) {
	id := getPathParam(r, "id")
	if id == "" {
		writeBadRequest(w, "Service ID is required")
		return
	}

	var req struct {
		Name        string `json:"name,omitempty"`
		Description string `json:"description,omitempty"`
	}
	if err := parseJSON(r, &req); err != nil {
		writeBadRequest(w, "Invalid request body: "+err.Error())
		return
	}

	h.mu.Lock()
	svc, ok := h.services[id]
	if !ok {
		h.mu.Unlock()
		writeNotFound(w, "Service not found")
		return
	}

	if req.Name != "" {
		svc.Name = req.Name
	}
	if req.Description != "" {
		svc.Description = req.Description
	}
	svc.UpdatedAt = time.Now()
	h.mu.Unlock()

	writeSuccess(w, svc)
}

// DeleteService handles DELETE /v1/services/{id}
func (h *SQLiteServiceHandler) DeleteService(w http.ResponseWriter, r *http.Request) {
	id := getPathParam(r, "id")
	if id == "" {
		writeBadRequest(w, "Service ID is required")
		return
	}

	h.mu.Lock()
	_, ok := h.services[id]
	if !ok {
		h.mu.Unlock()
		writeNotFound(w, "Service not found")
		return
	}
	delete(h.services, id)
	h.mu.Unlock()

	writeSuccess(w, map[string]string{
		"message": "Service deleted successfully",
	})
}
