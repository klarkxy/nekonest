package ws

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadJSON(t *testing.T) {
	var v struct {
		A string `json:"a"`
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"a":"b"}`))
	if err := readJSON(r, &v); err != nil || v.A != "b" {
		t.Fatalf("%v %#v", err, v)
	}

	r2 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{`))
	if err := readJSON(r2, &v); err == nil {
		t.Fatal("bad json")
	}

	// oversized body
	big := bytes.Repeat([]byte("x"), maxJSONBody+10)
	r3 := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(append([]byte(`{"a":"`), append(big, `"}`...)...)))
	if err := readJSON(r3, &v); err == nil {
		t.Fatal("want too large")
	}
}

func TestWriteJSON(t *testing.T) {
	rr := httptest.NewRecorder()
	writeJSON(rr, map[string]string{"ok": "1"})
	if !strings.Contains(rr.Body.String(), `"ok"`) {
		t.Fatalf("%s", rr.Body.String())
	}
}
