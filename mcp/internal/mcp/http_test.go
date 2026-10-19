package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"example.com/icloud-notes-mcp/internal/model"
)

type testSource struct{ snap model.Snapshot }

func (t testSource) Fetch(context.Context) (model.Snapshot, error) { return t.snap, nil }
func req(t *testing.T, s *Server, payload string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "http://localhost/mcp", strings.NewReader(payload))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func newTestServer() *Server {
	dt := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	return NewServer(testSource{snap: model.Snapshot{Folders: []model.Folder{{ID: "allowed", Name: "A"}}, Notes: []model.Note{{ID: "a", Title: "周报", FolderID: "allowed", Text: "今天完成了任务", ModifiedAt: &dt}, {ID: "b", Title: "无日期", FolderID: "allowed", Text: "xxx"}}}}, time.Second)
}
func TestInitializeAndDiscovery(t *testing.T) {
	s := newTestServer()
	w := req(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("2025-11-25")) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = req(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("get_notes_changed")) {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestWindowAndBatch(t *testing.T) {
	s := newTestServer()
	call := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_notes_changed","arguments":{"from":"2026-10-17T00:00:00Z","to":"2026-10-18T00:00:00Z"}}}`
	w := req(t, s, call)
	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(resp.Result.Content[0].Text), &body); err != nil {
		t.Fatal(err)
	}
	if body["total"] != float64(1) || body["unknown_modified_at_count"] != float64(1) {
		t.Fatal(body)
	}
	w = req(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_notes_batch","arguments":{"ids":["a","secret"],"max_chars_per_note":3}}}`)
	if !strings.Contains(w.Body.String(), "truncated") || strings.Contains(w.Body.String(), "secret text") {
		t.Fatal(w.Body.String())
	}
}
func TestOriginDenied(t *testing.T) {
	s := newTestServer()
	r := httptest.NewRequest("POST", "http://localhost/mcp", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://attacker.example")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
func TestHeaderMismatchDenied(t *testing.T) {
	s := newTestServer()
	r := httptest.NewRequest("POST", "http://localhost/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Mcp-Method", "tools/call")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "mismatch") {
		t.Fatal(w.Body.String())
	}
}
