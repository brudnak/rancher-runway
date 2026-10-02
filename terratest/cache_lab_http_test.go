package test

import (
	"bytes"
	"github.com/brudnak/ha-rancher-rke2/internal/cachelab"

	"encoding/json"

	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"

	"testing"
)

func TestCacheLabHTTPAuthorizationAndImport(t *testing.T) {
	s, w := cacheTestService(t)
	p := &localControlPanel{token: "test-control", cacheLab: s}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/cache-lab", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	response := httptest.NewRecorder()
	p.handleCacheLab(response, request)
	if response.Code != 403 {
		t.Fatal("unauthorized database library read accepted")
	}
	source := cacheTestDB(t, "CREATE TABLE records(id INTEGER PRIMARY KEY)", "INSERT INTO records VALUES(1)")
	content, _ := os.ReadFile(source)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "../sample.db")
	part.Write(content)
	writer.Close()
	request = httptest.NewRequest(http.MethodPost, "/api/cache-lab/import?workspace="+w.ID, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("X-Control-Panel-Token", p.token)
	response = httptest.NewRecorder()
	p.handleCacheLab(response, request)
	if response.Code != 200 {
		t.Fatalf("import: %d %s", response.Code, response.Body.String())
	}
	var snap cachelab.Snapshot
	json.Unmarshal(response.Body.Bytes(), &snap)
	if snap.Name != "sample.db" || snap.Tables != 1 {
		t.Fatalf("bad import: %+v", snap)
	}
	if s.Library().Job.Running || s.Library().Job.Error != "" {
		t.Fatal(s.Library().Job)
	}
}
