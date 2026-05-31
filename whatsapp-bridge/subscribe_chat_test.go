package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --------------------------------------------------------------------------
// SetChatWatched / IsChatWatched
// --------------------------------------------------------------------------

func TestIsChatWatchedDefaultFalse(t *testing.T) {
	store := newTestStore(t)
	jid := "111@s.whatsapp.net"
	store.StoreChat(jid, "Alice", time.Now())

	if store.IsChatWatched(jid) {
		t.Errorf("expected watched=false for newly stored chat, got true")
	}
}

func TestSetChatWatchedTrue(t *testing.T) {
	store := newTestStore(t)
	jid := "222@s.whatsapp.net"
	store.StoreChat(jid, "Bob", time.Now())

	if err := store.SetChatWatched(jid, true); err != nil {
		t.Fatalf("SetChatWatched true: %v", err)
	}
	if !store.IsChatWatched(jid) {
		t.Errorf("expected watched=true after SetChatWatched(true)")
	}
}

func TestSetChatWatchedFalse(t *testing.T) {
	store := newTestStore(t)
	jid := "333@s.whatsapp.net"
	store.StoreChat(jid, "Carol", time.Now())

	store.SetChatWatched(jid, true)
	if err := store.SetChatWatched(jid, false); err != nil {
		t.Fatalf("SetChatWatched false: %v", err)
	}
	if store.IsChatWatched(jid) {
		t.Errorf("expected watched=false after SetChatWatched(false)")
	}
}

func TestSetChatWatchedCreatesRowIfAbsent(t *testing.T) {
	store := newTestStore(t)
	jid := "444@g.us"

	// No prior StoreChat — SetChatWatched must create the row.
	if err := store.SetChatWatched(jid, true); err != nil {
		t.Fatalf("SetChatWatched on absent jid: %v", err)
	}
	if !store.IsChatWatched(jid) {
		t.Errorf("expected watched=true after SetChatWatched on new row")
	}
}

func TestIsChatWatchedReturnsFalseForAbsentJID(t *testing.T) {
	store := newTestStore(t)
	// Never stored — should return false, not panic or error.
	if store.IsChatWatched("nobody@s.whatsapp.net") {
		t.Errorf("expected false for JID not in DB")
	}
}

// --------------------------------------------------------------------------
// StoreChat upsert preserves watched flag
// --------------------------------------------------------------------------

func TestStoreChatUpsertPreservesWatched(t *testing.T) {
	store := newTestStore(t)
	jid := "555@g.us"
	ts := time.Now().Truncate(time.Second)

	store.StoreChat(jid, "Old Name", ts)
	store.SetChatWatched(jid, true)

	// A second StoreChat (e.g. from history sync) must NOT reset watched to 0.
	store.StoreChat(jid, "New Name", ts.Add(time.Minute))

	if !store.IsChatWatched(jid) {
		t.Errorf("StoreChat upsert reset watched flag to false — should preserve it")
	}

	// Confirm the name was updated.
	var name string
	store.db.QueryRow("SELECT name FROM chats WHERE jid = ?", jid).Scan(&name)
	if name != "New Name" {
		t.Errorf("expected name updated to 'New Name', got %q", name)
	}
}

// --------------------------------------------------------------------------
// /api/subscribe_chat HTTP handler
// --------------------------------------------------------------------------

func TestSubscribeChatEndpointMethodNotAllowed(t *testing.T) {
	store := newTestStore(t)
	srv := httptest.NewServer(buildRouter(nil, store))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/subscribe_chat")
	if err != nil {
		t.Fatalf("GET /api/subscribe_chat: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestSubscribeChatEndpointRejectsBadJSON(t *testing.T) {
	store := newTestStore(t)
	srv := httptest.NewServer(buildRouter(nil, store))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/subscribe_chat", "application/json", bytes.NewBufferString("not-json"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", resp.StatusCode)
	}
}

func TestSubscribeChatEndpointEmptyJIDReturns400(t *testing.T) {
	store := newTestStore(t)
	srv := httptest.NewServer(buildRouter(nil, store))
	defer srv.Close()

	body, _ := json.Marshal(SubscribeChatRequest{JID: "", Backfill: false})
	resp, err := http.Post(srv.URL+"/api/subscribe_chat", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for empty JID, got %d", resp.StatusCode)
	}
}

func TestSubscribeChatEndpointValidJIDReturns200(t *testing.T) {
	store := newTestStore(t)
	srv := httptest.NewServer(buildRouter(nil, store))
	defer srv.Close()

	jid := "120363000000000001@g.us"
	body, _ := json.Marshal(SubscribeChatRequest{JID: jid, Backfill: false})
	resp, err := http.Post(srv.URL+"/api/subscribe_chat", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for valid JID, got %d", resp.StatusCode)
	}

	var result SubscribeChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success=true, got message: %s", result.Message)
	}
	if !store.IsChatWatched(jid) {
		t.Errorf("expected chat to be watched after subscribe")
	}
}

// --------------------------------------------------------------------------
// /api/unsubscribe_chat HTTP handler
// --------------------------------------------------------------------------

func TestUnsubscribeChatEndpointMethodNotAllowed(t *testing.T) {
	store := newTestStore(t)
	srv := httptest.NewServer(buildRouter(nil, store))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/unsubscribe_chat")
	if err != nil {
		t.Fatalf("GET /api/unsubscribe_chat: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestUnsubscribeChatEndpointValidJIDReturns200(t *testing.T) {
	store := newTestStore(t)
	jid := "120363000000000001@g.us"

	// Subscribe first.
	store.SetChatWatched(jid, true)

	srv := httptest.NewServer(buildRouter(nil, store))
	defer srv.Close()

	body, _ := json.Marshal(UnsubscribeChatRequest{JID: jid})
	resp, err := http.Post(srv.URL+"/api/unsubscribe_chat", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result SubscribeChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success=true, got: %s", result.Message)
	}
	if store.IsChatWatched(jid) {
		t.Errorf("expected watched=false after unsubscribe")
	}
}

// --------------------------------------------------------------------------
// normalizeSubscribeJID
// --------------------------------------------------------------------------

func TestNormalizeSubscribeJIDBarePhone(t *testing.T) {
	jid, err := normalizeSubscribeJID(nil, "972501234567")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if jid != "972501234567@s.whatsapp.net" {
		t.Errorf("expected PN JID, got %q", jid)
	}
}

func TestNormalizeSubscribeJIDGroupJID(t *testing.T) {
	jid, err := normalizeSubscribeJID(nil, "120363000000000001@g.us")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if jid != "120363000000000001@g.us" {
		t.Errorf("expected group JID unchanged, got %q", jid)
	}
}

func TestNormalizeSubscribeJIDEmptyReturnsError(t *testing.T) {
	_, err := normalizeSubscribeJID(nil, "")
	if err == nil {
		t.Errorf("expected error for empty input, got nil")
	}
}

func TestNormalizeSubscribeJIDWhitespaceReturnsError(t *testing.T) {
	_, err := normalizeSubscribeJID(nil, "   ")
	if err == nil {
		t.Errorf("expected error for whitespace-only input, got nil")
	}
}
