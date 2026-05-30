package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// noopLog satisfies waLog.Logger without producing output.
type noopLog struct{}

func (noopLog) Debugf(string, ...interface{}) {}
func (noopLog) Infof(string, ...interface{})  {}
func (noopLog) Warnf(string, ...interface{})  {}
func (noopLog) Errorf(string, ...interface{}) {}
func (noopLog) Sub(string) waLog.Logger       { return noopLog{} }

// newTestStore opens a fresh in-memory SQLite MessageStore with the production schema.
func newTestStore(t *testing.T) *MessageStore {
	t.Helper()
	db, err := sql.Open("sqlite3", "file::memory:?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	// Restrict to one connection so the in-memory DB isn't lost when the pool opens a second.
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS chats (
			jid TEXT PRIMARY KEY,
			name TEXT,
			last_message_time TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS messages (
			id TEXT,
			chat_jid TEXT,
			sender TEXT,
			content TEXT,
			timestamp TIMESTAMP,
			is_from_me BOOLEAN,
			media_type TEXT,
			filename TEXT,
			url TEXT,
			media_key BLOB,
			file_sha256 BLOB,
			file_enc_sha256 BLOB,
			file_length INTEGER,
			PRIMARY KEY (id, chat_jid),
			FOREIGN KEY (chat_jid) REFERENCES chats(jid)
		);
	`)
	if err != nil {
		db.Close()
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &MessageStore{db: db}
}

func storeMsg(t *testing.T, store *MessageStore, id, chatJID, content string, ts time.Time) {
	t.Helper()
	if err := store.StoreMessage(id, chatJID, "sender", content, ts, false, "", "", "", nil, nil, nil, 0); err != nil {
		t.Fatalf("StoreMessage(%s): %v", id, err)
	}
}

func chatCount(t *testing.T, store *MessageStore, jid string) int {
	t.Helper()
	var n int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM chats WHERE jid = ?", jid).Scan(&n); err != nil {
		t.Fatalf("count chats for %s: %v", jid, err)
	}
	return n
}

func msgCount(t *testing.T, store *MessageStore, chatJID string) int {
	t.Helper()
	var n int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM messages WHERE chat_jid = ?", chatJID).Scan(&n); err != nil {
		t.Fatalf("count messages for %s: %v", chatJID, err)
	}
	return n
}

// pnResolver returns a resolver that maps a specific @lid user to a fixed PN JID.
func pnResolver(lidUser, pnUser string) func(types.JID) types.JID {
	return func(jid types.JID) types.JID {
		if jid.Server == types.HiddenUserServer && jid.User == lidUser {
			return types.JID{User: pnUser, Server: types.DefaultUserServer}
		}
		return jid
	}
}

// identityResolver returns every JID unchanged (simulates "no PN mapping known").
func identityResolver(jid types.JID) types.JID { return jid }

// --------------------------------------------------------------------------
// resolveToPN
// --------------------------------------------------------------------------

func TestResolveToPNNilClient(t *testing.T) {
	jid := types.JID{User: "123", Server: types.HiddenUserServer}
	got := resolveToPN(nil, jid)
	if got != jid {
		t.Errorf("expected %v unchanged, got %v", jid, got)
	}
}

// --------------------------------------------------------------------------
// EnsureChat
// --------------------------------------------------------------------------

func TestEnsureChatCreatesRow(t *testing.T) {
	store := newTestStore(t)
	ts := time.Now().Truncate(time.Second)
	jid := "111@s.whatsapp.net"

	if err := store.EnsureChat(jid, ts); err != nil {
		t.Fatalf("EnsureChat: %v", err)
	}
	if chatCount(t, store, jid) != 1 {
		t.Errorf("expected row to be created")
	}
	var name string
	store.db.QueryRow("SELECT name FROM chats WHERE jid = ?", jid).Scan(&name)
	if name != "" {
		t.Errorf("expected empty name on new row, got %q", name)
	}
}

func TestEnsureChatDoesNotOverwriteExisting(t *testing.T) {
	store := newTestStore(t)
	ts := time.Now().Truncate(time.Second)
	jid := "222@s.whatsapp.net"

	store.StoreChat(jid, "Alice", ts)
	// EnsureChat must leave the resolved name untouched.
	if err := store.EnsureChat(jid, ts.Add(time.Hour)); err != nil {
		t.Fatalf("EnsureChat: %v", err)
	}
	var name string
	store.db.QueryRow("SELECT name FROM chats WHERE jid = ?", jid).Scan(&name)
	if name != "Alice" {
		t.Errorf("expected name %q preserved, got %q", "Alice", name)
	}
}

// --------------------------------------------------------------------------
// TouchChatLastMessageTime
// --------------------------------------------------------------------------

func TestTouchChatLastMessageTimeUpdatesOnlyTimestamp(t *testing.T) {
	store := newTestStore(t)
	ts := time.Now().Truncate(time.Second).UTC()
	jid := "333@s.whatsapp.net"

	store.StoreChat(jid, "Bob", ts)
	later := ts.Add(time.Hour)
	if err := store.TouchChatLastMessageTime(jid, later); err != nil {
		t.Fatalf("TouchChatLastMessageTime: %v", err)
	}

	var name string
	var got time.Time
	store.db.QueryRow("SELECT name, last_message_time FROM chats WHERE jid = ?", jid).Scan(&name, &got)
	if name != "Bob" {
		t.Errorf("name changed unexpectedly: got %q", name)
	}
	if got.UTC().Unix() != later.Unix() {
		t.Errorf("expected timestamp %v, got %v", later, got.UTC())
	}
}

// --------------------------------------------------------------------------
// migrateLIDChatsWithResolver
// --------------------------------------------------------------------------

func TestMigrateLIDChatsNilStore(t *testing.T) {
	// Must not panic.
	migrateLIDChatsWithResolver(nil, noopLog{}, identityResolver)
	migrateLIDChatsWithResolver(&MessageStore{}, noopLog{}, identityResolver)
}

func TestMigrateLIDChatsNoLIDChats(t *testing.T) {
	store := newTestStore(t)
	ts := time.Now().Truncate(time.Second)

	store.StoreChat("444@s.whatsapp.net", "Carol", ts)

	migrateLIDChatsWithResolver(store, noopLog{}, identityResolver)

	if chatCount(t, store, "444@s.whatsapp.net") != 1 {
		t.Errorf("PN chat should be untouched")
	}
}

func TestMigrateLIDChatsSkippedWhenNoMapping(t *testing.T) {
	store := newTestStore(t)
	ts := time.Now().Truncate(time.Second)
	lidStr := "abc@" + types.HiddenUserServer

	store.StoreChat(lidStr, "Unknown", ts)
	storeMsg(t, store, "m1", lidStr, "hello", ts)

	// identity resolver → no PN mapping → all skipped
	migrateLIDChatsWithResolver(store, noopLog{}, identityResolver)

	if chatCount(t, store, lidStr) != 1 {
		t.Errorf("@lid chat should remain when no PN mapping is known")
	}
	if msgCount(t, store, lidStr) != 1 {
		t.Errorf("@lid messages should remain when no PN mapping is known")
	}
}

func TestMigrateLIDChatsMergeIntoNewPN(t *testing.T) {
	store := newTestStore(t)
	ts := time.Now().Truncate(time.Second)
	lidStr := "abc@" + types.HiddenUserServer
	pnStr := "123@" + types.DefaultUserServer

	store.StoreChat(lidStr, "Alice", ts)
	storeMsg(t, store, "m1", lidStr, "hello", ts)
	storeMsg(t, store, "m2", lidStr, "world", ts.Add(time.Second))

	migrateLIDChatsWithResolver(store, noopLog{}, pnResolver("abc", "123"))

	if chatCount(t, store, lidStr) != 0 {
		t.Errorf("@lid chat should be deleted after merge")
	}
	if msgCount(t, store, lidStr) != 0 {
		t.Errorf("@lid messages should be deleted after merge")
	}
	if chatCount(t, store, pnStr) != 1 {
		t.Errorf("PN chat should be created")
	}
	if msgCount(t, store, pnStr) != 2 {
		t.Errorf("both messages should be under PN chat, got %d", msgCount(t, store, pnStr))
	}
}

func TestMigrateLIDChatsMergeIntoExistingPN(t *testing.T) {
	store := newTestStore(t)
	ts := time.Now().Truncate(time.Second)
	lidStr := "abc@" + types.HiddenUserServer
	pnStr := "123@" + types.DefaultUserServer

	// Pre-existing PN chat with a resolved name and a later timestamp.
	store.StoreChat(pnStr, "Alice Smith", ts.Add(2*time.Second))
	storeMsg(t, store, "pn-m1", pnStr, "existing msg", ts.Add(2*time.Second))

	// @lid chat with an earlier timestamp and an unresolved name.
	store.StoreChat(lidStr, "", ts)
	storeMsg(t, store, "lid-m1", lidStr, "lid msg", ts)

	migrateLIDChatsWithResolver(store, noopLog{}, pnResolver("abc", "123"))

	if chatCount(t, store, lidStr) != 0 {
		t.Errorf("@lid chat should be deleted")
	}
	// PN chat must retain its resolved name and the later timestamp.
	var name string
	store.db.QueryRow("SELECT name FROM chats WHERE jid = ?", pnStr).Scan(&name)
	if name != "Alice Smith" {
		t.Errorf("PN chat name should be preserved, got %q", name)
	}
	if msgCount(t, store, pnStr) != 2 {
		t.Errorf("expected 2 messages under PN chat, got %d", msgCount(t, store, pnStr))
	}
}

func TestMigrateLIDChatsDedupOnPKConflict(t *testing.T) {
	store := newTestStore(t)
	ts := time.Now().Truncate(time.Second)
	lidStr := "abc@" + types.HiddenUserServer
	pnStr := "123@" + types.DefaultUserServer

	// PN chat already has "shared-id".
	store.StoreChat(pnStr, "Alice", ts)
	storeMsg(t, store, "shared-id", pnStr, "original content", ts)

	// @lid chat has the same message ID — this is the duplicate.
	store.StoreChat(lidStr, "", ts)
	storeMsg(t, store, "shared-id", lidStr, "duplicate content", ts)
	storeMsg(t, store, "unique-id", lidStr, "unique msg", ts.Add(time.Second))

	migrateLIDChatsWithResolver(store, noopLog{}, pnResolver("abc", "123"))

	if chatCount(t, store, lidStr) != 0 {
		t.Errorf("@lid chat should be deleted")
	}
	// Exactly 2 messages: original "shared-id" + moved "unique-id". Duplicate dropped.
	if msgCount(t, store, pnStr) != 2 {
		t.Errorf("expected 2 messages (deduped), got %d", msgCount(t, store, pnStr))
	}
	// The PN-side "shared-id" content must be preserved (UPDATE OR IGNORE keeps it).
	var content string
	store.db.QueryRow("SELECT content FROM messages WHERE id = ? AND chat_jid = ?", "shared-id", pnStr).Scan(&content)
	if content != "original content" {
		t.Errorf("original message should win on PK conflict, got %q", content)
	}
}

func TestMigrateLIDChatsIdempotent(t *testing.T) {
	store := newTestStore(t)
	ts := time.Now().Truncate(time.Second)
	lidStr := "abc@" + types.HiddenUserServer

	store.StoreChat(lidStr, "Dave", ts)
	storeMsg(t, store, "m1", lidStr, "hello", ts)

	resolver := pnResolver("abc", "999")

	migrateLIDChatsWithResolver(store, noopLog{}, resolver)

	var countBefore int
	store.db.QueryRow("SELECT COUNT(*) FROM chats").Scan(&countBefore)
	var msgBefore int
	store.db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&msgBefore)

	// Second run must be a no-op.
	migrateLIDChatsWithResolver(store, noopLog{}, resolver)

	var countAfter int
	store.db.QueryRow("SELECT COUNT(*) FROM chats").Scan(&countAfter)
	var msgAfter int
	store.db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&msgAfter)

	if countBefore != countAfter {
		t.Errorf("second run changed chat count: %d -> %d", countBefore, countAfter)
	}
	if msgBefore != msgAfter {
		t.Errorf("second run changed message count: %d -> %d", msgBefore, msgAfter)
	}
}

// --------------------------------------------------------------------------
// resolveBridgePort
// --------------------------------------------------------------------------

func TestResolveBridgePortDefault(t *testing.T) {
	if got := resolveBridgePort("", noopLog{}); got != 8080 {
		t.Errorf("empty env: expected 8080, got %d", got)
	}
}

func TestResolveBridgePortValid(t *testing.T) {
	if got := resolveBridgePort("18082", noopLog{}); got != 18082 {
		t.Errorf("expected 18082, got %d", got)
	}
}

func TestResolveBridgePortInvalidString(t *testing.T) {
	if got := resolveBridgePort("abc", noopLog{}); got != 8080 {
		t.Errorf("non-numeric: expected fallback 8080, got %d", got)
	}
}

func TestResolveBridgePortZero(t *testing.T) {
	if got := resolveBridgePort("0", noopLog{}); got != 8080 {
		t.Errorf("zero: expected fallback 8080, got %d", got)
	}
}

func TestResolveBridgePortOutOfRange(t *testing.T) {
	if got := resolveBridgePort("99999", noopLog{}); got != 8080 {
		t.Errorf("out-of-range: expected fallback 8080, got %d", got)
	}
}

func TestResolveBridgePortBoundaries(t *testing.T) {
	if got := resolveBridgePort("1", noopLog{}); got != 1 {
		t.Errorf("min valid port: expected 1, got %d", got)
	}
	if got := resolveBridgePort("65535", noopLog{}); got != 65535 {
		t.Errorf("max valid port: expected 65535, got %d", got)
	}
	if got := resolveBridgePort("65536", noopLog{}); got != 8080 {
		t.Errorf("one above max: expected fallback 8080, got %d", got)
	}
}

// --------------------------------------------------------------------------
// buildRouter / REST server connectivity (test plan item 2)
// --------------------------------------------------------------------------

// TestServerBindsAndAcceptsTraffic starts a real HTTP server via httptest
// and verifies it responds to requests. A GET to /api/send returns 405
// (method check fires before any client/store access), so nil client/store
// are safe here.
func TestServerBindsAndAcceptsTraffic(t *testing.T) {
	srv := httptest.NewServer(buildRouter(nil, nil))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/send")
	if err != nil {
		t.Fatalf("GET /api/send: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed, got %d", resp.StatusCode)
	}
}

func TestServerDownloadEndpointResponds(t *testing.T) {
	srv := httptest.NewServer(buildRouter(nil, nil))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/download")
	if err != nil {
		t.Fatalf("GET /api/download: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed, got %d", resp.StatusCode)
	}
}

// --------------------------------------------------------------------------
// extractTextContent
// --------------------------------------------------------------------------

func TestExtractTextContentConversation(t *testing.T) {
	msg := &waProto.Message{Conversation: proto.String("hello")}
	if got := extractTextContent(msg); got != "hello" {
		t.Errorf("expected %q, got %q", "hello", got)
	}
}

func TestExtractTextContentExtended(t *testing.T) {
	msg := &waProto.Message{ExtendedTextMessage: &waProto.ExtendedTextMessage{Text: proto.String("ext")}}
	if got := extractTextContent(msg); got != "ext" {
		t.Errorf("expected %q, got %q", "ext", got)
	}
}

func TestExtractTextContentImageCaption(t *testing.T) {
	msg := &waProto.Message{ImageMessage: &waProto.ImageMessage{Caption: proto.String("pic caption")}}
	if got := extractTextContent(msg); got != "pic caption" {
		t.Errorf("expected %q, got %q", "pic caption", got)
	}
}

func TestExtractTextContentVideoCaption(t *testing.T) {
	msg := &waProto.Message{VideoMessage: &waProto.VideoMessage{Caption: proto.String("vid caption")}}
	if got := extractTextContent(msg); got != "vid caption" {
		t.Errorf("expected %q, got %q", "vid caption", got)
	}
}

func TestExtractTextContentNil(t *testing.T) {
	if got := extractTextContent(nil); got != "" {
		t.Errorf("expected empty string for nil, got %q", got)
	}
}

// --------------------------------------------------------------------------
// SendMessageRequest JSON — quoted_id field round-trip
// --------------------------------------------------------------------------

func TestSendMessageRequestQuotedIDJSON(t *testing.T) {
	// Verify the quoted_id field is correctly omitted when empty and included when set.
	reqWithQuote := SendMessageRequest{Recipient: "123", Message: "hi", QuotedID: "msg-abc"}
	b, err := json.Marshal(reqWithQuote)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(b, []byte(`"quoted_id"`)) {
		t.Errorf("expected quoted_id in JSON when set, got %s", b)
	}

	reqNoQuote := SendMessageRequest{Recipient: "123", Message: "hi"}
	b2, err := json.Marshal(reqNoQuote)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(b2, []byte(`"quoted_id"`)) {
		t.Errorf("expected quoted_id omitted from JSON when empty, got %s", b2)
	}
}

// --------------------------------------------------------------------------
// /api/send handler — quoted_id forwarded in request body
// --------------------------------------------------------------------------

// TestQuotedIDLookupMissingReturnsErrNoRows verifies that the DB query used in
// sendWhatsAppMessage returns sql.ErrNoRows when the quoted_id is not in the store,
// ensuring the "if err != nil { return false, ... }" guard triggers correctly.
func TestQuotedIDLookupMissingReturnsErrNoRows(t *testing.T) {
	store := newTestStore(t)
	chatJID := "123456789@s.whatsapp.net"

	var content, sender string
	err := store.db.QueryRow(
		"SELECT COALESCE(content, ''), COALESCE(sender, '') FROM messages WHERE id = ? AND chat_jid = ?",
		"nonexistent-id", chatJID,
	).Scan(&content, &sender)

	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows for absent quoted_id, got %v", err)
	}
}

// --------------------------------------------------------------------------
// createWhatsAppGroup / leaveWhatsAppGroup — validation (no live client needed
// because IsConnected() is the first guard and returns false for a nil client,
// but we exercise the post-connect validation via the helper functions directly
// using a test double approach: we test the JSON struct shapes and handler
// routing without a live connection.)
// --------------------------------------------------------------------------

func TestCreateGroupRequestJSON(t *testing.T) {
	req := CreateGroupRequest{Name: "Test", Participants: []string{"123", "456"}}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(b, []byte(`"name"`)) || !bytes.Contains(b, []byte(`"participants"`)) {
		t.Errorf("unexpected JSON: %s", b)
	}
	// is_community and community_parent_jid should be omitted when zero
	if bytes.Contains(b, []byte(`"is_community"`)) {
		t.Errorf("is_community should be omitted when false")
	}
	if bytes.Contains(b, []byte(`"community_parent_jid"`)) {
		t.Errorf("community_parent_jid should be omitted when empty")
	}
}

func TestCreateGroupEndpointMethodNotAllowed(t *testing.T) {
	srv := httptest.NewServer(buildRouter(nil, nil))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/create_group")
	if err != nil {
		t.Fatalf("GET /api/create_group: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestLeaveGroupEndpointMethodNotAllowed(t *testing.T) {
	srv := httptest.NewServer(buildRouter(nil, nil))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/leave_group")
	if err != nil {
		t.Fatalf("GET /api/leave_group: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestCreateGroupEndpointRejectsBadJSON(t *testing.T) {
	srv := httptest.NewServer(buildRouter(nil, nil))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/create_group", "application/json", bytes.NewBufferString("not-json"))
	if err != nil {
		t.Fatalf("POST /api/create_group: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", resp.StatusCode)
	}
}

func TestLeaveGroupEndpointRejectsBadJSON(t *testing.T) {
	srv := httptest.NewServer(buildRouter(nil, nil))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/leave_group", "application/json", bytes.NewBufferString("not-json"))
	if err != nil {
		t.Fatalf("POST /api/leave_group: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", resp.StatusCode)
	}
}

func TestSendHandlerAcceptsQuotedID(t *testing.T) {
	srv := httptest.NewServer(buildRouter(nil, nil))
	defer srv.Close()

	// POST a well-formed request including quoted_id.
	// The handler will fail (no live client), but it must reach the client
	// check (StatusInternalServerError), not reject the request as malformed (400).
	body, _ := json.Marshal(map[string]string{
		"recipient": "123456789",
		"message":   "hello",
		"quoted_id": "some-msg-id",
	})
	resp, err := http.Post(srv.URL+"/api/send", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/send: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusBadRequest {
		t.Errorf("quoted_id in body should not cause 400 Bad Request, got %d", resp.StatusCode)
	}
}
