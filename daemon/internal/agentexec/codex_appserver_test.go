package agentexec

import (
	"encoding/json"
	"testing"
)

func TestParseThreadStartResponse(t *testing.T) {
	raw := json.RawMessage(`{
		"thread": {
			"id": "thr_abc",
			"sessionId": "ses_xyz",
			"cwd": "D:\\\\proj"
		},
		"cwd": "D:\\\\proj",
		"model": "x",
		"modelProvider": "y",
		"approvalPolicy": "never",
		"approvalsReviewer": "user",
		"sandbox": "workspace-write"
	}`)
	id, sid := parseThreadStartResponse(raw)
	if id != "thr_abc" || sid != "ses_xyz" {
		t.Fatalf("id=%q sid=%q", id, sid)
	}
}

func TestParseThreadStartResponseFlat(t *testing.T) {
	raw := json.RawMessage(`{"threadId":"t1","sessionId":"s1"}`)
	id, sid := parseThreadStartResponse(raw)
	if id != "t1" || sid != "s1" {
		t.Fatalf("id=%q sid=%q", id, sid)
	}
}
