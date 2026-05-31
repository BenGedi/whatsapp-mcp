package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateMediaPath(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("could not determine home directory: %v", err)
	}

	// Create a temp file inside the home directory for valid-path tests
	tmpFile, err := os.CreateTemp(homeDir, "test-media-*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	// Create a temp directory inside home for the "directory instead of file" case
	tmpDir, err := os.MkdirTemp(homeDir, "test-dir-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a symlink inside home pointing to /etc/passwd for the symlink-escape test.
	// May fail on Windows (no /etc/passwd, or elevated privileges required).
	symlinkPath := filepath.Join(homeDir, "test-evil-symlink")
	os.Remove(symlinkPath)
	symlinkErr := os.Symlink("/etc/passwd", symlinkPath)
	if symlinkErr == nil {
		t.Cleanup(func() { os.Remove(symlinkPath) })
	}

	tests := []struct {
		name        string
		path        string
		wantError   bool
		errContains string
		skip        bool
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
			name: "system file outside home directory",
			path: func() string {
				if runtime.GOOS == "windows" {
					return filepath.Join(os.Getenv("SYSTEMROOT"), "System32", "cmd.exe")
				}
				return "/etc/passwd"
			}(),
			wantError:   true,
			errContains: "media path must be within the user home directory",
		},
		{
			name:        "nonexistent path inside home directory",
			path:        filepath.Join(homeDir, "nonexistent-file-xyz.txt"),
			wantError:   true,
			errContains: "media file not found",
		},
		{
			name:        "symlink inside home pointing outside is rejected",
			path:        symlinkPath,
			wantError:   true,
			errContains: "media path must be within the user home directory",
			skip:        symlinkErr != nil,
		},
		{
			name:        "directory instead of file",
			path:        tmpDir,
			wantError:   true,
			errContains: "media path must point to a file",
		},
		{
			name:      "valid absolute file path inside home directory",
			path:      tmpFile.Name(),
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skip {
				t.Skip("skipped: prerequisite setup failed")
			}
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
			name:     "JID with colon replaced by underscore",
			jid:      "123456789:0@s.whatsapp.net",
			wantSafe: "123456789_0@s.whatsapp.net",
		},
		{
			name:     "JID with embedded double dot is allowed (not a traversal segment)",
			jid:      "foo..bar@s.whatsapp.net",
			wantSafe: "foo..bar@s.whatsapp.net",
		},
		{
			name:      "bare double dot is rejected",
			jid:       "..",
			wantError: true,
		},
		{
			name:      "path traversal sequence",
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
			name:      "JID whose colon replacement does not yield bare double dot",
			jid:       ".:.@s.whatsapp.net",
			wantSafe:  "._.@s.whatsapp.net",
			wantError: false,
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
