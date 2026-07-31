package ws

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxAttachmentBytes = 4 << 20 // 4 MB
)

var allowedMIME = map[string]bool{
	"image/jpeg":       true,
	"image/jpg":        true,
	"image/png":        true,
	"image/webp":       true,
	"image/gif":        true,
	"text/plain":       true,
	"text/markdown":    true,
	"application/pdf":  true,
	"application/json": true,
}

type attachmentMeta struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	MIME      string `json:"mime"`
	Size      int64  `json:"size"`
	DeviceID  string `json:"device_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Created   int64  `json:"created"`
	Ext       string `json:"ext,omitempty"`
}

func (s *Server) attachmentsDir() string {
	if s.dataDir == "" {
		return filepath.Join("data", "attachments")
	}
	return filepath.Join(s.dataDir, "attachments")
}

func (s *Server) handleAttachments(w http.ResponseWriter, r *http.Request) {
	// /api/attachments  POST
	// /api/attachments/{id} GET
	path := strings.TrimPrefix(r.URL.Path, "/api/attachments")
	path = strings.Trim(path, "/")

	if path == "" {
		if r.Method == http.MethodPost {
			s.handleAttachmentUpload(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Method == http.MethodGet {
		s.handleAttachmentGet(w, r, path)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleAttachmentUpload(w http.ResponseWriter, r *http.Request) {
	if !s.requirePhoneAuth(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAttachmentBytes+1<<20)
	if err := r.ParseMultipartForm(maxAttachmentBytes + 1<<20); err != nil {
		http.Error(w, "file too large or invalid multipart", http.StatusBadRequest)
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Read all (capped)
	data, err := io.ReadAll(io.LimitReader(file, maxAttachmentBytes+1))
	if err != nil {
		http.Error(w, "read failed", http.StatusBadRequest)
		return
	}
	if int64(len(data)) > maxAttachmentBytes {
		http.Error(w, "file exceeds 4MB limit", http.StatusRequestEntityTooLarge)
		return
	}

	mime := hdr.Header.Get("Content-Type")
	if mime == "" || mime == "application/octet-stream" {
		mime = http.DetectContentType(data)
	}
	mime = strings.ToLower(strings.TrimSpace(strings.Split(mime, ";")[0]))
	if !allowedMIME[mime] {
		ext := strings.ToLower(filepath.Ext(hdr.Filename))
		okExt := map[string]string{
			".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png",
			".webp": "image/webp", ".gif": "image/gif", ".txt": "text/plain",
			".md": "text/markdown", ".pdf": "application/pdf", ".json": "application/json",
		}
		if m, ok := okExt[ext]; ok {
			mime = m
		} else {
			http.Error(w, "unsupported file type: "+mime, http.StatusBadRequest)
			return
		}
	}

	id, err := randomHex(16)
	if err != nil {
		http.Error(w, "id gen failed", http.StatusInternalServerError)
		return
	}
	key, err := randomHex(16)
	if err != nil {
		http.Error(w, "key gen failed", http.StatusInternalServerError)
		return
	}

	ext := filepath.Ext(hdr.Filename)
	if ext == "" {
		ext = extFromMIME(mime)
	}
	safeName := sanitizeFilename(hdr.Filename)
	if safeName == "" {
		safeName = "file" + ext
	}

	dir := s.attachmentsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	// Prune old attachments opportunistically (quota / disk safety).
	go pruneAttachments(dir, 7*24*time.Hour, 2000)

	diskPath := filepath.Join(dir, id+ext)
	if err := os.WriteFile(diskPath, data, 0o600); err != nil {
		http.Error(w, "write failed", http.StatusInternalServerError)
		return
	}

	meta := attachmentMeta{
		ID:        id,
		Key:       key,
		Name:      safeName,
		MIME:      mime,
		Size:      int64(len(data)),
		DeviceID:  r.FormValue("device_id"),
		SessionID: r.FormValue("session_id"),
		Created:   time.Now().Unix(),
		Ext:       ext,
	}
	mb, _ := json.Marshal(meta)
	// Meta must not collide with JSON attachment bodies (id.json).
	if err := os.WriteFile(filepath.Join(dir, id+".meta.json"), mb, 0o600); err != nil {
		os.Remove(diskPath)
		http.Error(w, "meta write failed", http.StatusInternalServerError)
		return
	}

	urlPath := fmt.Sprintf("/api/attachments/%s?k=%s", id, key)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":   id,
		"url":  urlPath,
		"name": safeName,
		"mime": mime,
		"size": len(data),
		"key":  key,
	})
}

func (s *Server) handleAttachmentGet(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" || strings.Contains(id, "/") || strings.Contains(id, "..") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	key := r.URL.Query().Get("k")
	dir := s.attachmentsDir()
	mb, err := os.ReadFile(filepath.Join(dir, id+".meta.json"))
	if err != nil {
		// Backward compat: older uploads used id.json for meta (broke application/json files).
		mb, err = os.ReadFile(filepath.Join(dir, id+".json"))
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
	}
	var meta attachmentMeta
	if err := json.Unmarshal(mb, &meta); err != nil {
		http.Error(w, "corrupt meta", http.StatusInternalServerError)
		return
	}

	authed := false
	if s.phoneSecret == "" {
		authed = true
	} else if key != "" && subtleConstEq(key, meta.Key) {
		authed = true // capability URL for daemon download
	} else {
		cred := phoneSecretFromRequest(r)
		if secureEqual(cred, s.phoneSecret) {
			authed = true
		} else if auth, err := s.db.ValidatePhoneToken(cred); err == nil {
			// Phone token: require grant when attachment is device-scoped.
			if meta.DeviceID == "" || s.phoneMayAccessDevice(auth, meta.DeviceID) {
				authed = true
			}
		}
	}
	if !authed {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	diskPath := filepath.Join(dir, id+meta.Ext)
	f, err := os.Open(diskPath)
	if err != nil {
		matches, _ := filepath.Glob(filepath.Join(dir, id+".*"))
		for _, m := range matches {
			if strings.HasSuffix(m, ".json") {
				continue
			}
			f, err = os.Open(m)
			if err == nil {
				diskPath = m
				break
			}
		}
	}
	if err != nil || f == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	if meta.MIME != "" {
		w.Header().Set("Content-Type", meta.MIME)
	}
	dispName := strings.ReplaceAll(meta.Name, `"`, "")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, dispName))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, meta.Name, time.Unix(meta.Created, 0), f)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, "..", "")
	name = strings.Map(func(r rune) rune {
		if r < 32 {
			return -1
		}
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		return r
	}, name)
	if len(name) > 120 {
		name = name[:120]
	}
	return strings.TrimSpace(name)
}

func extFromMIME(mime string) string {
	switch strings.ToLower(mime) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	case "text/markdown":
		return ".md"
	case "application/json":
		return ".json"
	default:
		return ".bin"
	}
}

func subtleConstEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// pruneAttachments deletes meta+payload pairs older than maxAge, and trims
// oldest files when more than maxFiles metas exist.
func pruneAttachments(dir string, maxAge time.Duration, maxFiles int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type item struct {
		id   string
		mod  time.Time
		meta string
	}
	var items []item
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		var id string
		if strings.HasSuffix(name, ".meta.json") {
			id = strings.TrimSuffix(name, ".meta.json")
		} else if strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".meta.json") {
			// legacy meta only if no matching payload with same basename as body
			id = strings.TrimSuffix(name, ".json")
		} else {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		metaPath := filepath.Join(dir, name)
		if info.ModTime().Before(cutoff) {
			removeAttachmentPair(dir, id)
			continue
		}
		items = append(items, item{id: id, mod: info.ModTime(), meta: metaPath})
	}
	if maxFiles <= 0 || len(items) <= maxFiles {
		return
	}
	// Sort oldest first
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].mod.Before(items[i].mod) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	drop := len(items) - maxFiles
	for i := 0; i < drop; i++ {
		removeAttachmentPair(dir, items[i].id)
	}
}

func removeAttachmentPair(dir, id string) {
	// Remove meta + payload (any ext), including legacy id.json meta and id.json body.
	matches, _ := filepath.Glob(filepath.Join(dir, id+".*"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
}
