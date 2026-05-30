package main

import (
	"os"
	"strings"
	"testing"
)

func TestValidateMediaPath(t *testing.T) {
	// Create a temporary file for valid-path tests
	tmpFile, err := os.CreateTemp("", "test-media-*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	tests := []struct {
		name        string
		path        string
		wantError   bool
		errContains string
	}{
		{
			name:        "empty path",
			path:        "",
			wantError:   true,
			errContains: "media path cannot be empty",
		},
		{
			name:        "relative path",
			path:        "relative/path/file.txt",
			wantError:   true,
			errContains: "media path must be absolute",
		},
		{
			name:        "path traversal via relative path",
			path:        "../etc/passwd",
			wantError:   true,
			errContains: "media path must be absolute",
		},
		{
			name:        "nonexistent absolute path",
			path:        "/nonexistent/path/to/file.txt",
			wantError:   true,
			errContains: "media file not found",
		},
		{
			name:        "directory instead of file",
			path:        "/tmp",
			wantError:   true,
			errContains: "media path must point to a file",
		},
		{
			name:      "valid absolute file path",
			path:      tmpFile.Name(),
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMediaPath(tt.path)
			if tt.wantError {
				if err == nil {
					t.Errorf("expected error but got nil")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestSanitizeChatJIDForPath(t *testing.T) {
	tests := []struct {
		name      string
		jid       string
		wantSafe  string
		wantError bool
	}{
		{
			name:     "normal JID without colon",
			jid:      "123456789@s.whatsapp.net",
			wantSafe: "123456789@s.whatsapp.net",
		},
		{
			name:     "JID with colon (colon replaced by underscore)",
			jid:      "123456789:0@s.whatsapp.net",
			wantSafe: "123456789_0@s.whatsapp.net",
		},
		{
			name:      "path traversal via double dot",
			jid:       "../../etc/passwd",
			wantError: true,
		},
		{
			name:      "JID with forward slash",
			jid:       "chat/evil@s.whatsapp.net",
			wantError: true,
		},
		{
			name:      "JID with backslash",
			jid:       "chat\\evil@s.whatsapp.net",
			wantError: true,
		},
		{
			name:      "JID starting with double dot",
			jid:       "..@s.whatsapp.net",
			wantError: true,
		},
		{
			name:      "double dot embedded in JID",
			jid:       "foo..bar@s.whatsapp.net",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			safe, err := sanitizeChatJIDForPath(tt.jid)
			if tt.wantError {
				if err == nil {
					t.Errorf("expected error for JID %q but got safe=%q", tt.jid, safe)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for JID %q: %v", tt.jid, err)
				}
				if safe != tt.wantSafe {
					t.Errorf("for JID %q: expected %q, got %q", tt.jid, tt.wantSafe, safe)
				}
			}
		})
	}
}
