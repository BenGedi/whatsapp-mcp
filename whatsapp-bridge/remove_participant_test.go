package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// mockGroupClient implements groupParticipantClient for unit tests.
type mockGroupClient struct {
	connected    bool
	groupInfo    *types.GroupInfo
	groupInfoErr error
	updateErr    error
}

func (m *mockGroupClient) IsConnected() bool { return m.connected }

func (m *mockGroupClient) GetGroupInfo(_ context.Context, _ types.JID) (*types.GroupInfo, error) {
	return m.groupInfo, m.groupInfoErr
}

func (m *mockGroupClient) UpdateGroupParticipants(_ context.Context, _ types.JID, _ []types.JID, _ whatsmeow.ParticipantChange) ([]types.GroupParticipant, error) {
	return nil, m.updateErr
}

// participantPN builds a GroupParticipant whose primary JID is a phone-number JID.
func participantPN(phone string) types.GroupParticipant {
	pn := types.JID{User: phone, Server: types.DefaultUserServer}
	return types.GroupParticipant{JID: pn, PhoneNumber: pn}
}

// participantLID builds a GroupParticipant whose primary JID is a LID (@lid) JID.
func participantLID(lid, phone string) types.GroupParticipant {
	lidJID := types.JID{User: lid, Server: types.HiddenUserServer}
	pnJID := types.JID{User: phone, Server: types.DefaultUserServer}
	return types.GroupParticipant{JID: lidJID, LID: lidJID, PhoneNumber: pnJID}
}

const testGroupJID = "120363000000000001@g.us"
const testParticipant = "972501234567"

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

// --------------------------------------------------------------------------
// HTTP routing
// --------------------------------------------------------------------------

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

// --------------------------------------------------------------------------
// Input validation (nil client safe — returns before any client call)
// --------------------------------------------------------------------------

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
			participant: testParticipant,
			wantCode:    http.StatusBadRequest,
			wantMsg:     "group_jid is required",
		},
		{
			name:        "whitespace-only group_jid",
			groupJID:    "   ",
			participant: testParticipant,
			wantCode:    http.StatusBadRequest,
			wantMsg:     "group_jid is required",
		},
		{
			name:        "empty participant",
			groupJID:    testGroupJID,
			participant: "",
			wantCode:    http.StatusBadRequest,
			wantMsg:     "participant is required",
		},
		{
			name:        "whitespace-only participant",
			groupJID:    testGroupJID,
			participant: "   ",
			wantCode:    http.StatusBadRequest,
			wantMsg:     "participant is required",
		},
		{
			name:        "group_jid not @g.us",
			groupJID:    "972501234567@s.whatsapp.net",
			participant: testParticipant,
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

// --------------------------------------------------------------------------
// Mock-client tests (participant lookup + server errors)
// --------------------------------------------------------------------------

func TestRemoveParticipantNotConnected(t *testing.T) {
	mock := &mockGroupClient{connected: false}
	resp := removeWhatsAppGroupParticipant(mock, testGroupJID, testParticipant)
	if resp.Success {
		t.Fatal("expected failure")
	}
	if !strings.Contains(resp.Message, "Not connected") {
		t.Errorf("unexpected message: %q", resp.Message)
	}
}

func TestRemoveParticipantGetGroupInfoError(t *testing.T) {
	mock := &mockGroupClient{
		connected:    true,
		groupInfoErr: errors.New("network error"),
	}
	resp := removeWhatsAppGroupParticipant(mock, testGroupJID, testParticipant)
	if resp.Success {
		t.Fatal("expected failure")
	}
	if !strings.Contains(resp.Message, "Could not fetch group info") {
		t.Errorf("unexpected message: %q", resp.Message)
	}
}

func TestRemoveParticipantNotInGroup(t *testing.T) {
	mock := &mockGroupClient{
		connected: true,
		groupInfo: &types.GroupInfo{
			Participants: []types.GroupParticipant{
				participantPN("999999999999"), // someone else, not our target
			},
		},
	}
	resp := removeWhatsAppGroupParticipant(mock, testGroupJID, testParticipant)
	if resp.Success {
		t.Fatal("expected failure")
	}
	if !strings.Contains(resp.Message, "not found in group") {
		t.Errorf("unexpected message: %q", resp.Message)
	}
	if !resp.clientError {
		t.Errorf("expected clientError=true so handler returns HTTP 400")
	}
}

func TestRemoveParticipantNotInGroup_LIDMember(t *testing.T) {
	// Group has a LID-based participant; removal by phone number should still work.
	mock := &mockGroupClient{
		connected: true,
		groupInfo: &types.GroupInfo{
			Participants: []types.GroupParticipant{
				participantLID("237726507004029", testParticipant),
			},
		},
	}
	resp := removeWhatsAppGroupParticipant(mock, testGroupJID, testParticipant)
	if !resp.Success {
		t.Fatalf("expected success for LID-based participant, got: %s", resp.Message)
	}
	if !strings.Contains(resp.Message, "237726507004029@lid") {
		t.Errorf("expected LID JID in success message, got: %q", resp.Message)
	}
}

func TestRemoveParticipantNotAdmin(t *testing.T) {
	// UpdateGroupParticipants returns an error (simulates not-authorized IQ error from server).
	mock := &mockGroupClient{
		connected: true,
		groupInfo: &types.GroupInfo{
			Participants: []types.GroupParticipant{
				participantPN(testParticipant),
			},
		},
		updateErr: errors.New("not-authorized"),
	}
	resp := removeWhatsAppGroupParticipant(mock, testGroupJID, testParticipant)
	if resp.Success {
		t.Fatal("expected failure")
	}
	if !strings.Contains(resp.Message, "Error removing participant") {
		t.Errorf("unexpected message: %q", resp.Message)
	}
	// Server-side auth error is not a clientError — handler should return HTTP 500.
	if resp.clientError {
		t.Errorf("expected clientError=false so handler returns HTTP 500 (not 400)")
	}
}
