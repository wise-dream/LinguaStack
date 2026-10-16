package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/httpapi"
)

func TestHealthReportsOK(t *testing.T) {
	srv := httpapi.NewServer(httpapi.Deps{Cfg: config.Settings{}})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("код = %d, ожидалось 200", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("ответ не разобрался как JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, ожидалось \"ok\"", body["status"])
	}
}

func TestUnknownRouteIsNotFound(t *testing.T) {
	srv := httpapi.NewServer(httpapi.Deps{Cfg: config.Settings{}})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("код = %d, ожидалось 404", rec.Code)
	}
}
