package attach

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Ref is a remote attachment reference from the phone.
type Ref struct {
	URL  string
	Name string
	MIME string
	ID   string
}

// LocalFile is an attachment downloaded to a daemon-owned temporary directory.
// Path is absolute and remains valid until the resumed agent process exits.
type LocalFile struct {
	Path string
	Name string
	MIME string
}

// Materialize downloads attachments into a temp dir and returns local paths + prompt suffix.
func Materialize(serverWSURL string, sessionID string, refs []Ref) (dir string, files []LocalFile, promptSuffix string, err error) {
	if len(refs) == 0 {
		return "", nil, "", nil
	}
	baseHTTP := wsToHTTP(serverWSURL)
	allowedHost, err := hostOf(baseHTTP)
	if err != nil || allowedHost == "" {
		return "", nil, "", fmt.Errorf("invalid server URL for attachments: %w", err)
	}
	dir, err = os.MkdirTemp("", "nekonest-att-"+sanitize(sessionID)+"-")
	if err != nil {
		return "", nil, "", err
	}
	client := newSafeHTTPClient(allowedHost)
	var b strings.Builder
	b.WriteString("\n\n[NekoNest attachments — local files on this PC]\n")
	for i, ref := range refs {
		u := absolutize(baseHTTP, ref.URL)
		if u == "" {
			continue
		}
		if err := validateAttachmentURL(u, allowedHost); err != nil {
			b.WriteString(fmt.Sprintf("- %s: rejected (%v)\n", ref.Name, err))
			continue
		}
		name := ref.Name
		if name == "" {
			name = fmt.Sprintf("file_%d", i+1)
			if ext := extFromMIME(ref.MIME); ext != "" {
				name += ext
			}
		}
		name = filepath.Base(name)
		local := filepath.Join(dir, name)
		// avoid overwrite
		if _, stErr := os.Stat(local); stErr == nil {
			local = filepath.Join(dir, fmt.Sprintf("%d_%s", i+1, name))
		}
		if dlErr := downloadTo(client, u, local); dlErr != nil {
			b.WriteString(fmt.Sprintf("- %s: download failed (%v)\n", name, dlErr))
			continue
		}
		files = append(files, LocalFile{
			Path: local,
			Name: name,
			MIME: ref.MIME,
		})
		b.WriteString(fmt.Sprintf("- %s (%s) → %s\n", name, ref.MIME, local))
	}
	if len(files) == 0 {
		if cleanupErr := os.RemoveAll(dir); cleanupErr != nil {
			return dir, nil, "", fmt.Errorf(
				"all %d attachments failed; remove temporary directory: %w",
				len(refs),
				cleanupErr,
			)
		}
		return "", nil, "", fmt.Errorf("all %d attachments failed to download", len(refs))
	}
	b.WriteString("Please inspect these local files if relevant to the user request.\n")
	return dir, files, b.String(), nil
}

func newSafeHTTPClient(allowedHost string) *http.Client {
	transport := &http.Transport{
		Proxy: nil, // never honor HTTP(S)_PROXY for attachment fetches
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Timeout:   60 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return validateAttachmentURL(req.URL.String(), allowedHost)
		},
	}
}

func downloadTo(client *http.Client, rawURL, dest string) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(dest)
		}
	}()
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 5<<20)); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

func wsToHTTP(serverURL string) string {
	u := strings.TrimSpace(serverURL)
	u = strings.Replace(u, "wss://", "https://", 1)
	u = strings.Replace(u, "ws://", "http://", 1)
	// strip trailing /ws paths if any
	u = strings.TrimSuffix(u, "/")
	u = strings.TrimSuffix(u, "/ws")
	u = strings.TrimSuffix(u, "/")
	return u
}

func hostOf(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Host == "" {
		return "", fmt.Errorf("missing host")
	}
	return strings.ToLower(u.Host), nil
}

// validateAttachmentURL restricts fetches to the NekoNest server attachment API only.
func validateAttachmentURL(raw, allowedHost string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("bad url")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return fmt.Errorf("scheme not allowed")
	}
	host := strings.ToLower(u.Host)
	if host == "" || host != strings.ToLower(allowedHost) {
		return fmt.Errorf("host not allowed")
	}
	path := u.EscapedPath()
	if path == "" {
		path = u.Path
	}
	if !strings.HasPrefix(path, "/api/attachments/") {
		return fmt.Errorf("path not allowed")
	}
	// id segment only — no nested paths
	rest := strings.TrimPrefix(path, "/api/attachments/")
	if rest == "" || strings.Contains(rest, "/") || strings.Contains(rest, "..") {
		return fmt.Errorf("invalid attachment id")
	}
	return nil
}

func absolutize(base, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	if strings.HasPrefix(ref, "/") {
		bu, err := url.Parse(base)
		if err != nil || bu.Scheme == "" {
			return ""
		}
		return bu.Scheme + "://" + bu.Host + ref
	}
	// relative non-root paths are not accepted for attachments
	return ""
}

func sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
	if len(s) > 24 {
		s = s[:24]
	}
	return s
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
	default:
		return ""
	}
}
