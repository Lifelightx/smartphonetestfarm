package coordinator_server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestAutomationRESTEndpoints(t *testing.T) {
	cfg := LoadConfig()
	db, err := OpenDB(cfg.PostgresURI)
	if err != nil {
		t.Fatalf("failed to open database for test: %v", err)
	}

	server := NewServer(cfg, db)

	// 1. Create a script
	scriptID := uuid.New().String()
	yamlContent := `
steps:
  - launch:
      package: com.example.test
  - click:
      resourceId: com.example.test:id/button
`
	reqBody := map[string]interface{}{
		"id":      scriptID,
		"name":    "Test REST Script",
		"content": yamlContent,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/automation/scripts", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	server.handleScripts(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected StatusOK on script create, got %d", resp.StatusCode)
	}

	var createResp map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&createResp)
	if createResp["success"] != true || createResp["id"] != scriptID {
		t.Errorf("unexpected script create response: %+v", createResp)
	}

	// 2. Get script by ID
	req = httptest.NewRequest(http.MethodGet, "/api/v1/automation/scripts/"+scriptID, nil)
	w = httptest.NewRecorder()

	server.handleScriptByID(w, req)

	resp = w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected StatusOK on script get, got %d", resp.StatusCode)
	}

	var getResp map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&getResp)
	if getResp["id"] != scriptID || getResp["name"] != "Test REST Script" || getResp["content"] != yamlContent {
		t.Errorf("unexpected script get response: %+v", getResp)
	}

	// 3. List scripts
	req = httptest.NewRequest(http.MethodGet, "/api/v1/automation/scripts", nil)
	w = httptest.NewRecorder()

	server.handleScripts(w, req)

	resp = w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected StatusOK on script list, got %d", resp.StatusCode)
	}

	var listResp []map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&listResp)
	found := false
	for _, s := range listResp {
		if s["id"] == scriptID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected script %s to be in the list, got: %+v", scriptID, listResp)
	}

	// 4. Delete script by ID
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/automation/scripts/"+scriptID, nil)
	w = httptest.NewRecorder()

	server.handleScriptByID(w, req)

	resp = w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected StatusOK on script delete, got %d", resp.StatusCode)
	}

	var deleteResp map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&deleteResp)
	if deleteResp["success"] != true {
		t.Errorf("unexpected script delete response: %+v", deleteResp)
	}

	// 5. Verify script is deleted
	req = httptest.NewRequest(http.MethodGet, "/api/v1/automation/scripts/"+scriptID, nil)
	w = httptest.NewRecorder()

	server.handleScriptByID(w, req)

	resp = w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected StatusNotFound on deleted script get, got %d", resp.StatusCode)
	}
}
