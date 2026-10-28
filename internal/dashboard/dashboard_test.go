package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOverview(t *testing.T) {
	for _, test := range []struct {
		name   string
		method string
		err    error
		status int
	}{
		{name: "overview", method: http.MethodGet, status: http.StatusOK},
		{name: "database unavailable", method: http.MethodGet, err: errors.New("secret database connection string"), status: http.StatusServiceUnavailable},
		{name: "read only", method: http.MethodPost, status: http.StatusMethodNotAllowed},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := NewHandler(func(ctx context.Context) (json.RawMessage, error) {
				if _, ok := ctx.Deadline(); !ok {
					t.Error("database request must have a deadline")
				}
				return json.RawMessage(`{"listings":[],"searches":[]}`), test.err
			})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(test.method, "/api/overview", nil))
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if test.status == http.StatusOK {
				if !json.Valid(response.Body.Bytes()) || response.Header().Get("Content-Type") != "application/json" {
					t.Fatal("expected JSON response")
				}
				if response.Header().Get("Cache-Control") != "no-store" {
					t.Error("dashboard data must not be cached")
				}
			}
			if test.err != nil && response.Body.String() != "Dashboard data is unavailable. Check the database and migrations.\n" {
				t.Error("unexpected error response; internal errors must not be exposed")
			}
		})
	}
}
