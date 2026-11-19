package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Paulo31209/devops-e2e-platform/internal/notes"
)

type response struct {
	status int
	header http.Header
	body   []byte
}

func newTestServer(t *testing.T) (*API, *httptest.Server) {
	t.Helper()
	api := New(notes.NewStore(), slog.New(slog.NewTextHandler(io.Discard, nil)), "test")
	srv := httptest.NewServer(api.Routes())
	t.Cleanup(srv.Close)
	return api, srv
}

func do(t *testing.T, method, url, body string) response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response{status: resp.StatusCode, header: resp.Header, body: b}
}

func TestProbes(t *testing.T) {
	api, srv := newTestServer(t)

	if got := do(t, http.MethodGet, srv.URL+"/healthz", "").status; got != http.StatusOK {
		t.Fatalf("/healthz = %d", got)
	}
	if got := do(t, http.MethodGet, srv.URL+"/readyz", "").status; got != http.StatusOK {
		t.Fatalf("/readyz = %d", got)
	}

	api.SetReady(false)
	if got := do(t, http.MethodGet, srv.URL+"/readyz", "").status; got != http.StatusServiceUnavailable {
		t.Fatalf("/readyz after SetReady(false) = %d", got)
	}
}

func TestVersion(t *testing.T) {
	_, srv := newTestServer(t)
	resp := do(t, http.MethodGet, srv.URL+"/version", "")
	if resp.status != http.StatusOK || !strings.Contains(string(resp.body), `"version":"test"`) {
		t.Fatalf("/version = %d %s", resp.status, resp.body)
	}
}

func TestNotesLifecycle(t *testing.T) {
	_, srv := newTestServer(t)
	base := srv.URL + "/api/v1/notes"

	resp := do(t, http.MethodPost, base, `{"title":"deploy","body":"ship it"}`)
	if resp.status != http.StatusCreated {
		t.Fatalf("create = %d", resp.status)
	}
	if loc := resp.header.Get("Location"); loc != "/api/v1/notes/1" {
		t.Fatalf("Location = %q", loc)
	}
	var created notes.Note
	if err := json.Unmarshal(resp.body, &created); err != nil {
		t.Fatal(err)
	}
	if created.ID != 1 || created.Title != "deploy" {
		t.Fatalf("created = %+v", created)
	}

	if got := do(t, http.MethodGet, base+"/1", "").status; got != http.StatusOK {
		t.Fatalf("get = %d", got)
	}

	var list []notes.Note
	if err := json.Unmarshal(do(t, http.MethodGet, base, "").body, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list length = %d", len(list))
	}

	if got := do(t, http.MethodDelete, base+"/1", "").status; got != http.StatusNoContent {
		t.Fatalf("delete = %d", got)
	}
	if got := do(t, http.MethodGet, base+"/1", "").status; got != http.StatusNotFound {
		t.Fatalf("get after delete = %d", got)
	}
	if got := do(t, http.MethodDelete, base+"/1", "").status; got != http.StatusNotFound {
		t.Fatalf("delete after delete = %d", got)
	}
}

func TestCreateValidation(t *testing.T) {
	_, srv := newTestServer(t)
	base := srv.URL + "/api/v1/notes"

	tests := []struct {
		name string
		body string
		want int
	}{
		{"invalid json", `{`, http.StatusBadRequest},
		{"unknown field", `{"title":"x","extra":1}`, http.StatusBadRequest},
		{"missing title", `{"body":"x"}`, http.StatusUnprocessableEntity},
		{"blank title", `{"title":"   "}`, http.StatusUnprocessableEntity},
		{"title too long", `{"title":"` + strings.Repeat("a", maxTitleLength+1) + `"}`, http.StatusUnprocessableEntity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := do(t, http.MethodPost, base, tt.body).status; got != tt.want {
				t.Fatalf("status = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestInvalidID(t *testing.T) {
	_, srv := newTestServer(t)
	for _, id := range []string{"abc", "0", "-1"} {
		if got := do(t, http.MethodGet, srv.URL+"/api/v1/notes/"+id, "").status; got != http.StatusBadRequest {
			t.Fatalf("GET id=%s = %d, want 400", id, got)
		}
	}
}

func TestMetrics(t *testing.T) {
	_, srv := newTestServer(t)
	do(t, http.MethodGet, srv.URL+"/api/v1/notes", "")
	do(t, http.MethodGet, srv.URL+"/does-not-exist", "")

	out := string(do(t, http.MethodGet, srv.URL+"/metrics", "").body)
	for _, want := range []string{
		`http_requests_total{code="200",method="GET",route="/api/v1/notes"} 1`,
		`http_requests_total{code="404",method="GET",route="unmatched"} 1`,
		`app_build_info{version="test"} 1`,
		`notes_stored 0`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}
