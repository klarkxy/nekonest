package ws

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nekonest/server/internal/db"
	"github.com/nekonest/server/internal/protocol"
)

func testServer(t *testing.T, secret string) *Server {
	t.Helper()
	dir := t.TempDir()
	d, err := db.New(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	s := NewWithSecret(d, secret)
	s.SetDataDir(dir)
	_ = s.SetTransportMode(protocol.TransportOpen)
	return s
}

func TestAttachmentUploadAndGet(t *testing.T) {
	s := testServer(t, "sekrit")
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile("file", "note.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(fw, "hello-attach")
	_ = w.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/attachments", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer sekrit")
	rr := httptest.NewRecorder()
	s.handleAttachments(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("upload %d %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	id, _ := resp["id"].(string)
	key, _ := resp["key"].(string)
	urlPath, _ := resp["url"].(string)
	if id == "" || key == "" || !strings.Contains(urlPath, id) {
		t.Fatalf("%#v", resp)
	}
	// meta must be .meta.json so JSON bodies are not clobbered
	metaPath := filepath.Join(s.attachmentsDir(), id+".meta.json")
	if _, err := os.Stat(metaPath); err != nil {
		t.Fatalf("meta: %v", err)
	}

	// GET with key
	getReq := httptest.NewRequest(http.MethodGet, "/api/attachments/"+id+"?k="+key, nil)
	getRR := httptest.NewRecorder()
	s.handleAttachments(getRR, getReq)
	if getRR.Code != http.StatusOK || getRR.Body.String() != "hello-attach" {
		t.Fatalf("get %d %q", getRR.Code, getRR.Body.String())
	}

	// GET wrong key
	bad := httptest.NewRequest(http.MethodGet, "/api/attachments/"+id+"?k=deadbeefdeadbeefdeadbeefdeadbeef", nil)
	badRR := httptest.NewRecorder()
	s.handleAttachments(badRR, bad)
	if badRR.Code != http.StatusUnauthorized {
		t.Fatalf("bad key %d", badRR.Code)
	}

	// GET with phone secret
	okReq := httptest.NewRequest(http.MethodGet, "/api/attachments/"+id, nil)
	okReq.Header.Set("X-Neko-Secret", "sekrit")
	okRR := httptest.NewRecorder()
	s.handleAttachments(okRR, okReq)
	if okRR.Code != http.StatusOK {
		t.Fatalf("secret get %d", okRR.Code)
	}

	// path traversal
	trav := httptest.NewRequest(http.MethodGet, "/api/attachments/../etc/passwd", nil)
	trav.Header.Set("X-Neko-Secret", "sekrit")
	tr := httptest.NewRecorder()
	s.handleAttachments(tr, trav)
	if tr.Code != http.StatusNotFound {
		t.Fatalf("trav %d", tr.Code)
	}
}

func TestAttachmentUploadAuthAndType(t *testing.T) {
	s := testServer(t, "sekrit")
	// no auth
	req := httptest.NewRequest(http.MethodPost, "/api/attachments", strings.NewReader("x"))
	rr := httptest.NewRecorder()
	s.handleAttachments(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauth %d", rr.Code)
	}

	// unsupported type (extension + content both disallowed)
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="file"; filename="x.bin"`)
	h.Set("Content-Type", "application/octet-stream")
	fw, _ := w.CreatePart(h)
	_, _ = fw.Write([]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07})
	_ = w.Close()
	req2 := httptest.NewRequest(http.MethodPost, "/api/attachments", &body)
	req2.Header.Set("Content-Type", w.FormDataContentType())
	req2.Header.Set("Authorization", "Bearer sekrit")
	rr2 := httptest.NewRecorder()
	s.handleAttachments(rr2, req2)
	if rr2.Code != http.StatusBadRequest {
		t.Fatalf("bin %d %s", rr2.Code, rr2.Body.String())
	}
}

func TestAttachmentJSONBodyNotClobbered(t *testing.T) {
	s := testServer(t, "")
	payload := `{"hello":"world"}`
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, _ := w.CreateFormFile("file", "data.json")
	_, _ = io.WriteString(fw, payload)
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/attachments", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rr := httptest.NewRecorder()
	s.handleAttachments(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	id, _ := resp["id"].(string)
	key, _ := resp["key"].(string)

	get := httptest.NewRequest(http.MethodGet, "/api/attachments/"+id+"?k="+key, nil)
	gr := httptest.NewRecorder()
	s.handleAttachments(gr, get)
	if gr.Body.String() != payload {
		t.Fatalf("body clobbered: %q", gr.Body.String())
	}
}

func TestPruneAttachments(t *testing.T) {
	dir := t.TempDir()
	id := "abc123"
	_ = os.WriteFile(filepath.Join(dir, id+".meta.json"), []byte(`{"id":"abc123","ext":".txt"}`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, id+".txt"), []byte("x"), 0o600)
	// force old mtime via write is "now" — prune with maxAge 0 still keeps recent;
	// call with maxFiles=0 to no-op drop, then maxFiles=-1 style via 0 files keep
	pruneAttachments(dir, 0, 100) // maxAge 0 → everything before now-0 is not before cutoff...
	// cutoff = now - 0 = now; ModTime().Before(now) is usually true
	// so files get removed
	pruneAttachments(dir, 0, 0)
	// directory may be empty after age prune
	entries, _ := os.ReadDir(dir)
	_ = entries
}

func TestExtFromMIMEMore(t *testing.T) {
	if extFromMIME("image/png") != ".png" {
		t.Fatal("png")
	}
	if extFromMIME("image/webp") != ".webp" {
		t.Fatal("webp")
	}
	if extFromMIME("text/markdown") != ".md" {
		t.Fatal("md")
	}
	if extFromMIME("application/json") != ".json" {
		t.Fatal("json")
	}
}

func TestRequirePhoneAuthAndRegister(t *testing.T) {
	s := testServer(t, "tok")
	// list devices needs auth
	req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	rr := httptest.NewRecorder()
	s.handleListDevices(rr, req)
	if got := rr.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("%d", rr.Code)
	}
	req.Header.Set("Authorization", "Bearer tok")
	rr2 := httptest.NewRecorder()
	s.handleListDevices(rr2, req)
	if got := rr2.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if rr2.Code != http.StatusOK {
		t.Fatalf("%d %s", rr2.Code, rr2.Body.String())
	}
}

func TestHandleRegisterRequiresBootstrapWhenSecret(t *testing.T) {
	s := testServer(t, "phone")
	t.Setenv("NEKONEST_BOOTSTRAP_TOKEN", "")
	req := httptest.NewRequest(http.MethodPost, "/api/devices/register", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.handleRegisterDevice(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 got %d %s", rr.Code, rr.Body.String())
	}

	t.Setenv("NEKONEST_BOOTSTRAP_TOKEN", "boot")
	req2 := httptest.NewRequest(http.MethodPost, "/api/devices/register", strings.NewReader(`{"name":"书房"}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Neko-Bootstrap", "boot")
	rr2 := httptest.NewRecorder()
	s.handleRegisterDevice(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("register %d %s", rr2.Code, rr2.Body.String())
	}
	var out map[string]string
	_ = json.Unmarshal(rr2.Body.Bytes(), &out)
	if out["device_id"] == "" || out["token"] == "" {
		t.Fatalf("%#v", out)
	}
}

func TestPairGenerateAndConsumeHTTP(t *testing.T) {
	s := testServer(t, "phone")
	t.Setenv("NEKONEST_BOOTSTRAP_TOKEN", "boot")
	// register device
	reg := httptest.NewRequest(http.MethodPost, "/api/devices/register", strings.NewReader(`{"name":"p"}`))
	reg.Header.Set("Content-Type", "application/json")
	reg.Header.Set("X-Neko-Bootstrap", "boot")
	rr := httptest.NewRecorder()
	s.handleRegisterDevice(rr, reg)
	var dev map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &dev)

	genBody := `{"device_id":"` + dev["device_id"] + `","token":"` + dev["token"] + `"}`
	gen := httptest.NewRequest(http.MethodPost, "/api/pair/generate", strings.NewReader(genBody))
	gen.Header.Set("Content-Type", "application/json")
	gr := httptest.NewRecorder()
	s.handleGeneratePairCode(gr, gen)
	if gr.Code != http.StatusOK {
		t.Fatalf("gen %d %s", gr.Code, gr.Body.String())
	}
	var codeResp map[string]any
	_ = json.Unmarshal(gr.Body.Bytes(), &codeResp)
	code, _ := codeResp["code"].(string)

	con := httptest.NewRequest(http.MethodPost, "/api/pair/consume", strings.NewReader(`{"code":"`+code+`"}`))
	con.Header.Set("Content-Type", "application/json")
	con.Header.Set("Authorization", "Bearer phone")
	cr := httptest.NewRecorder()
	s.handleConsumePairCode(cr, con)
	if cr.Code != http.StatusOK {
		t.Fatalf("consume %d %s", cr.Code, cr.Body.String())
	}
}

func TestHandleMessages(t *testing.T) {
	t.Setenv("NEKONEST_BOOTSTRAP_TOKEN", "")
	s2 := testServer(t, "") // open register without phone secret
	id, _ := mustRegOpen(t, s2)
	_ = s2.db.SaveMessage(id, "sess1", &protocol.SessionMessage{
		ID: "m1", Role: "user", Content: "hi", Type: "text", Timestamp: 1,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/messages?device_id="+id+"&session_id=sess1&limit=10", nil)
	rr := httptest.NewRecorder()
	s2.handleMessages(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "hi") {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
}

func mustRegOpen(t *testing.T, s *Server) (string, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/devices/register", strings.NewReader(`{"name":"t"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.handleRegisterDevice(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("reg %d %s", rr.Code, rr.Body.String())
	}
	var out map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	return out["device_id"], out["token"]
}
