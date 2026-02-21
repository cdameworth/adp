// Package handlers provides HTTP handlers for the ADP REST API.
package handlers

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/adp/adp/internal/infrastructure/database"
)

// Common response types

type ErrorResponse struct {
	Error   string      `json:"error"`
	Code    string      `json:"code,omitempty"`
	Details interface{} `json:"details,omitempty"`
}

type SuccessResponse struct {
	Data    interface{} `json:"data"`
	Message string      `json:"message,omitempty"`
}

type PaginatedResponse struct {
	Data       interface{} `json:"data"`
	Pagination Pagination  `json:"pagination"`
}

type Pagination struct {
	Total  int64 `json:"total"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
}

// Response helpers

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeSuccess(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusOK, SuccessResponse{Data: data})
}

func writeCreated(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusCreated, SuccessResponse{Data: data})
}

// ListResponse is the standard paginated list format expected by the frontend.
type ListResponse struct {
	Items  interface{} `json:"items"`
	Total  int         `json:"total"`
	Limit  int         `json:"limit"`
	Offset int         `json:"offset"`
}

// writeList writes a paginated list response matching the frontend ListResponse<T> contract.
// If total is -1, it is inferred from the slice length (no separate count query).
func writeList(w http.ResponseWriter, items interface{}, total, limit, offset int) {
	if items == nil {
		items = []interface{}{}
	}
	if total < 0 {
		total = sliceLen(items)
	}
	writeJSON(w, http.StatusOK, ListResponse{
		Items:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

// sliceLen returns the length of an interface that holds a slice.
func sliceLen(v interface{}) int {
	if v == nil {
		return 0
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		return rv.Len()
	}
	return 0
}

func writeError(w http.ResponseWriter, status int, message string, code string) {
	writeJSON(w, status, ErrorResponse{
		Error: message,
		Code:  code,
	})
}

func writeBadRequest(w http.ResponseWriter, message string) {
	writeError(w, http.StatusBadRequest, message, "bad_request")
}

func writeNotFound(w http.ResponseWriter, message string) {
	writeError(w, http.StatusNotFound, message, "not_found")
}

func writeUnauthorized(w http.ResponseWriter, message string) {
	writeError(w, http.StatusUnauthorized, message, "unauthorized")
}

func writeForbidden(w http.ResponseWriter, message string) {
	writeError(w, http.StatusForbidden, message, "forbidden")
}

func writeInternalError(w http.ResponseWriter, message string) {
	writeError(w, http.StatusInternalServerError, message, "internal_error")
}

func writeConflict(w http.ResponseWriter, message string) {
	writeError(w, http.StatusConflict, message, "conflict")
}

// Request parsing helpers

func parseJSON(r *http.Request, v interface{}) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func getPathParam(r *http.Request, name string) string {
	return r.PathValue(name)
}

func getQueryParam(r *http.Request, name string, defaultValue string) string {
	value := r.URL.Query().Get(name)
	if value == "" {
		return defaultValue
	}
	return value
}

func getQueryParamInt(r *http.Request, name string, defaultValue int) int {
	value := r.URL.Query().Get(name)
	if value == "" {
		return defaultValue
	}
	var result int
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return defaultValue
	}
	return result
}

// parseIntParam parses an integer query parameter with a default value.
func parseIntParam(r *http.Request, name string, defaultValue int) int {
	value := r.URL.Query().Get(name)
	if value == "" {
		return defaultValue
	}
	result, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return result
}

// isNotFoundError checks if the error is a not found error.
func isNotFoundError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not found")
}

// isAlreadyExistsError checks if the error is an already exists error.
func isAlreadyExistsError(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "already exists") ||
		err == database.ErrAlreadyExists)
}

// formatRelativeTime formats a time as a relative string.
func formatRelativeTime(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	switch {
	case diff < time.Minute:
		return "Just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return strconv.Itoa(mins) + " minutes ago"
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return strconv.Itoa(hours) + " hours ago"
	case diff < 48*time.Hour:
		return "Yesterday"
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		return strconv.Itoa(days) + " days ago"
	default:
		return t.Format("Jan 2, 2006")
	}
}
