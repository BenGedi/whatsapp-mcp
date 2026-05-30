package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// makeRemoveReq fires a POST to /api/remove_participant via the test router.
func makeRemoveReq(t *testing.T, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/remove_participant", &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	// nil client/store: validation cases return before any client access.
	buildRouter(nil, nil).ServeHTTP(rec, req)
	return rec
}

func TestRemoveParticipantMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/remove_participant", nil)
	rec := httptest.NewRecorder()
	buildRouter(nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestRemoveParticipantBadJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/remove_participant", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	buildRouter(nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", rec.Code)
	}
}

func TestRemoveParticipantValidation(t *testing.T) {
	tests := []struct {
		name        string
		groupJID    string
		participant string
		wantCode    int
		wantMsg     string
	}{
		{
			name:        "empty group_jid",
			groupJID:    "",
			participant: "972501234567",
			wantCode:    http.StatusBadRequest,
			wantMsg:     "group_jid is required",
		},
		{
			name:        "whitespace-only group_jid",
			groupJID:    "   ",
			participant: "972501234567",
			wantCode:    http.StatusBadRequest,
			wantMsg:     "group_jid is required",
		},
		{
			name:        "empty participant",
			groupJID:    "120363000000000001@g.us",
			participant: "",
			wantCode:    http.StatusBadRequest,
			wantMsg:     "participant is required",
		},
		{
			name:        "whitespace-only participant",
			groupJID:    "120363000000000001@g.us",
			participant: "   ",
			wantCode:    http.StatusBadRequest,
			wantMsg:     "participant is required",
		},
		{
			name:        "group_jid not @g.us",
			groupJID:    "972501234567@s.whatsapp.net",
			participant: "972509876543",
			wantCode:    http.StatusBadRequest,
			wantMsg:     "group_jid must end in @g.us",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := makeRemoveReq(t, map[string]string{
				"group_jid":   tt.groupJID,
				"participant": tt.participant,
			})
			if rec.Code != tt.wantCode {
				t.Errorf("expected HTTP %d, got %d (body: %s)", tt.wantCode, rec.Code, rec.Body.String())
			}
			var resp map[string]any
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if resp["success"] != false {
				t.Errorf("expected success=false")
			}
			msg, _ := resp["message"].(string)
			if !strings.Contains(msg, tt.wantMsg) {
				t.Errorf("expected message containing %q, got %q", tt.wantMsg, msg)
			}
		})
	}
}

// TestRemoveParticipantNotInGroup and TestRemoveParticipantNotAdmin require a
// live WhatsApp connection (GetGroupInfo and UpdateGroupParticipants talk to
// the WhatsApp servers). They are covered by the live test run documented in
// the PR test plan.
