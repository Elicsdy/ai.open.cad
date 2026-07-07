package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aiopencad/backend/internal/config"
	"aiopencad/backend/internal/llm"
	"aiopencad/backend/internal/store"
)

func TestFrontendServedUnderAPIPrefix(t *testing.T) {
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<div id=\"app\"></div>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "app.js"), []byte("console.log('ok')"), 0o644); err != nil {
		t.Fatal(err)
	}

	server := NewServer(nil, nil, config.Config{FrontendDist: dist})

	rootReq := httptest.NewRequest(http.MethodGet, "/", nil)
	rootResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(rootResp, rootReq)
	if rootResp.Code != http.StatusOK || !strings.Contains(rootResp.Body.String(), `id="app"`) {
		t.Fatalf("unexpected root response: status=%d body=%q", rootResp.Code, rootResp.Body.String())
	}

	indexReq := httptest.NewRequest(http.MethodGet, apiPrefix+"/", nil)
	indexResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(indexResp, indexReq)
	if indexResp.Code != http.StatusOK || !strings.Contains(indexResp.Body.String(), `id="app"`) {
		t.Fatalf("unexpected index response: status=%d body=%q", indexResp.Code, indexResp.Body.String())
	}

	noSlashReq := httptest.NewRequest(http.MethodGet, apiPrefix, nil)
	noSlashResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(noSlashResp, noSlashReq)
	if noSlashResp.Code != http.StatusFound || noSlashResp.Header().Get("Location") != apiPrefix+"/" {
		t.Fatalf("unexpected prefix redirect: status=%d location=%q", noSlashResp.Code, noSlashResp.Header().Get("Location"))
	}

	assetReq := httptest.NewRequest(http.MethodGet, apiPrefix+"/app.js", nil)
	assetResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(assetResp, assetReq)
	if assetResp.Code != http.StatusOK || !strings.Contains(assetResp.Body.String(), "console.log") {
		t.Fatalf("unexpected asset response: status=%d body=%q", assetResp.Code, assetResp.Body.String())
	}

	strippedAssetReq := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	strippedAssetResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(strippedAssetResp, strippedAssetReq)
	if strippedAssetResp.Code != http.StatusOK || !strings.Contains(strippedAssetResp.Body.String(), "console.log") {
		t.Fatalf("unexpected stripped asset response: status=%d body=%q", strippedAssetResp.Code, strippedAssetResp.Body.String())
	}

	duplicatedAssetReq := httptest.NewRequest(http.MethodGet, apiPrefix+apiPrefix+"/app.js", nil)
	duplicatedAssetResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(duplicatedAssetResp, duplicatedAssetReq)
	if duplicatedAssetResp.Code != http.StatusOK || !strings.Contains(duplicatedAssetResp.Body.String(), "console.log") {
		t.Fatalf("unexpected duplicated-prefix asset response: status=%d body=%q", duplicatedAssetResp.Code, duplicatedAssetResp.Body.String())
	}
}

func TestAPIRoutesAcceptStrippedGatewayPrefix(t *testing.T) {
	server := NewServer(nil, nil, config.Config{})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp := httptest.NewRecorder()
	server.Routes().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected stripped health status: %d", resp.Code)
	}

	duplicatedReq := httptest.NewRequest(http.MethodGet, apiPrefix+apiPrefix+"/health", nil)
	duplicatedResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(duplicatedResp, duplicatedReq)
	if duplicatedResp.Code != http.StatusOK {
		t.Fatalf("unexpected duplicated-prefix health status: %d", duplicatedResp.Code)
	}
}

func TestProjectRoutesAreClientScoped(t *testing.T) {
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	server := NewServer(db, nil, config.Config{})

	createReq := httptest.NewRequest(http.MethodPost, "/projects", strings.NewReader(`{"title":"Alice","prompt":"a","code":"Box(1,1,1,true);","language":"cascade-js"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("X-AI-OpenCAD-Client-ID", "alice")
	createResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("unexpected create status: %d body=%s", createResp.Code, createResp.Body.String())
	}

	aliceReq := httptest.NewRequest(http.MethodGet, "/projects", nil)
	aliceReq.Header.Set("X-AI-OpenCAD-Client-ID", "alice")
	aliceResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(aliceResp, aliceReq)
	if !strings.Contains(aliceResp.Body.String(), "Alice") {
		t.Fatalf("expected alice project, got %s", aliceResp.Body.String())
	}

	bobReq := httptest.NewRequest(http.MethodGet, "/projects", nil)
	bobReq.Header.Set("X-AI-OpenCAD-Client-ID", "bob")
	bobResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(bobResp, bobReq)
	if strings.Contains(bobResp.Body.String(), "Alice") {
		t.Fatalf("expected bob list to be isolated, got %s", bobResp.Body.String())
	}
}

func TestAsyncGenerateCADJob(t *testing.T) {
	server := NewServer(nil, llm.NewClient(config.LLMConfig{}), config.Config{
		LLM: config.LLMConfig{Timeout: time.Second},
	})

	req := httptest.NewRequest(http.MethodPost, "/generate-cad-async", strings.NewReader(`{"prompt":"make a box","language":"cascade-js"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	server.Routes().ServeHTTP(resp, req)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("unexpected async status: %d body=%s", resp.Code, resp.Body.String())
	}

	var created asyncJobSnapshot
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("expected job id")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		jobReq := httptest.NewRequest(http.MethodGet, "/jobs/"+created.ID, nil)
		jobResp := httptest.NewRecorder()
		server.Routes().ServeHTTP(jobResp, jobReq)
		if jobResp.Code != http.StatusOK {
			t.Fatalf("unexpected job status: %d body=%s", jobResp.Code, jobResp.Body.String())
		}

		var job asyncJobSnapshot
		if err := json.Unmarshal(jobResp.Body.Bytes(), &job); err != nil {
			t.Fatal(err)
		}
		if job.Status == "done" {
			if job.Result == nil {
				t.Fatal("expected job result")
			}
			if len(job.Events) == 0 {
				t.Fatal("expected job events")
			}
			return
		}
		if job.Status == "failed" {
			t.Fatalf("job failed: %s", job.Error)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job did not finish")
}

func TestAsyncGenerateCADFromImageJob(t *testing.T) {
	server := NewServer(nil, llm.NewClient(config.LLMConfig{}), config.Config{
		LLM: config.LLMConfig{Timeout: time.Second},
	})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("prompt", "make a bracket from this drawing"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("language", "cascade-js"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("image", "drawing.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/generate-cad-from-image-async", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp := httptest.NewRecorder()
	server.Routes().ServeHTTP(resp, req)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("unexpected async image status: %d body=%s", resp.Code, resp.Body.String())
	}

	var created asyncJobSnapshot
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Kind != "generate-cad-from-image" {
		t.Fatalf("unexpected job snapshot: %+v", created)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		jobReq := httptest.NewRequest(http.MethodGet, "/jobs/"+created.ID, nil)
		jobResp := httptest.NewRecorder()
		server.Routes().ServeHTTP(jobResp, jobReq)
		if jobResp.Code != http.StatusOK {
			t.Fatalf("unexpected job status: %d body=%s", jobResp.Code, jobResp.Body.String())
		}

		var job asyncJobSnapshot
		if err := json.Unmarshal(jobResp.Body.Bytes(), &job); err != nil {
			t.Fatal(err)
		}
		if job.Status == "done" {
			if job.Result == nil {
				t.Fatal("expected image job result")
			}
			return
		}
		if job.Status == "failed" {
			t.Fatalf("job failed: %s", job.Error)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("image job did not finish")
}

func TestAsyncJobStream(t *testing.T) {
	server := NewServer(nil, llm.NewClient(config.LLMConfig{}), config.Config{
		LLM: config.LLMConfig{Timeout: time.Second},
	})

	req := httptest.NewRequest(http.MethodPost, "/generate-cad-async", strings.NewReader(`{"prompt":"make a box","language":"cascade-js"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	server.Routes().ServeHTTP(resp, req)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("unexpected async status: %d body=%s", resp.Code, resp.Body.String())
	}

	var created asyncJobSnapshot
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	streamReq := httptest.NewRequest(http.MethodGet, "/jobs/"+created.ID+"/stream", nil)
	streamResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(streamResp, streamReq)

	if streamResp.Code != http.StatusOK {
		t.Fatalf("unexpected stream status: %d body=%s", streamResp.Code, streamResp.Body.String())
	}
	if got := streamResp.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("unexpected content type: %s", got)
	}
	body := streamResp.Body.String()
	if !strings.Contains(body, "event: event") {
		t.Fatalf("expected job event in stream, got %q", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Fatalf("expected done event in stream, got %q", body)
	}
}

func TestAsyncJobRequiresSameClient(t *testing.T) {
	server := NewServer(nil, llm.NewClient(config.LLMConfig{}), config.Config{
		LLM: config.LLMConfig{Timeout: time.Second},
	})

	req := httptest.NewRequest(http.MethodPost, "/generate-cad-async", strings.NewReader(`{"prompt":"make a box","language":"cascade-js"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AI-OpenCAD-Client-ID", "alice")
	resp := httptest.NewRecorder()
	server.Routes().ServeHTTP(resp, req)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("unexpected async status: %d body=%s", resp.Code, resp.Body.String())
	}

	var created asyncJobSnapshot
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	bobReq := httptest.NewRequest(http.MethodGet, "/jobs/"+created.ID, nil)
	bobReq.Header.Set("X-AI-OpenCAD-Client-ID", "bob")
	bobResp := httptest.NewRecorder()
	server.Routes().ServeHTTP(bobResp, bobReq)
	if bobResp.Code != http.StatusNotFound {
		t.Fatalf("expected hidden job for different client, got %d body=%s", bobResp.Code, bobResp.Body.String())
	}
}
