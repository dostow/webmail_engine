package cachekey

import (
	"testing"
)

func TestMessageKeyBuilder(t *testing.T) {
	tests := []struct {
		name      string
		accountID string
		folder    string
		uid       string
		wantKey   string
		wantErr   bool
	}{
		{
			name:      "valid message key",
			accountID: "acc_1",
			folder:    "INBOX",
			uid:       "343218",
			wantKey:   "msg:acc_1:INBOX:343218",
			wantErr:   false,
		},
		{
			name:      "valid message key with different folder",
			accountID: "acc_1",
			folder:    "Sent",
			uid:       "123",
			wantKey:   "msg:acc_1:Sent:123",
			wantErr:   false,
		},
		{
			name:      "empty account ID",
			accountID: "",
			folder:    "INBOX",
			uid:       "123",
			wantKey:   "",
			wantErr:   true,
		},
		{
			name:      "empty folder",
			accountID: "acc_1",
			folder:    "",
			uid:       "123",
			wantKey:   "",
			wantErr:   true,
		},
		{
			name:      "empty UID",
			accountID: "acc_1",
			folder:    "INBOX",
			uid:       "",
			wantKey:   "",
			wantErr:   true,
		},
		{
			name:      "invalid account ID format",
			accountID: "invalid",
			folder:    "INBOX",
			uid:       "123",
			wantKey:   "",
			wantErr:   true,
		},
		{
			name:      "non-numeric UID",
			accountID: "acc_1",
			folder:    "INBOX",
			uid:       "abc",
			wantKey:   "",
			wantErr:   true,
		},
		{
			name:      "zero UID",
			accountID: "acc_1",
			folder:    "INBOX",
			uid:       "0",
			wantKey:   "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := NewMessageKeyBuilder().
				AccountID(tt.accountID).
				Folder(tt.folder).
				UID(tt.uid).
				Build()

			if (err != nil) != tt.wantErr {
				t.Errorf("Build() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if key != tt.wantKey {
				t.Errorf("Build() key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}

func TestMessageKey(t *testing.T) {
	tests := []struct {
		name      string
		accountID string
		folder    string
		uid       string
		wantKey   string
		wantErr   bool
	}{
		{
			name:      "valid key",
			accountID: "acc_1",
			folder:    "INBOX",
			uid:       "343218",
			wantKey:   "msg:acc_1:INBOX:343218",
			wantErr:   false,
		},
		{
			name:      "empty folder",
			accountID: "acc_1",
			folder:    "",
			uid:       "123",
			wantKey:   "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := MessageKey(tt.accountID, tt.folder, tt.uid)
			if (err != nil) != tt.wantErr {
				t.Errorf("MessageKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if key != tt.wantKey {
				t.Errorf("MessageKey() key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}

func TestMessageKeySafe(t *testing.T) {
	tests := []struct {
		name      string
		accountID string
		folder    string
		uid       string
		wantKey   string
	}{
		{
			name:      "valid key with explicit folder",
			accountID: "acc_1",
			folder:    "INBOX",
			uid:       "343218",
			wantKey:   "msg:acc_1:INBOX:343218",
		},
		{
			name:      "empty folder defaults to INBOX",
			accountID: "acc_1",
			folder:    "",
			uid:       "123",
			wantKey:   "msg:acc_1:INBOX:123",
		},
		{
			name:      "Sent folder",
			accountID: "acc_1",
			folder:    "Sent",
			uid:       "456",
			wantKey:   "msg:acc_1:Sent:456",
		},
		{
			name:      "invalid account ID returns empty",
			accountID: "invalid",
			folder:    "INBOX",
			uid:       "123",
			wantKey:   "",
		},
		{
			name:      "empty UID returns empty",
			accountID: "acc_1",
			folder:    "INBOX",
			uid:       "",
			wantKey:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := MessageKeySafe(tt.accountID, tt.folder, tt.uid)
			if key != tt.wantKey {
				t.Errorf("MessageKeySafe() key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}

func TestBuildMust(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("BuildMust() did not panic on invalid input")
		}
	}()

	// This should panic
	NewMessageKeyBuilder().
		AccountID("").
		Folder("INBOX").
		UID("123").
		BuildMust()
}

func TestBuildOrDefault(t *testing.T) {
	tests := []struct {
		name       string
		accountID  string
		folder     string
		uid        string
		defaultKey string
		wantKey    string
	}{
		{
			name:       "valid key returns built key",
			accountID:  "acc_1",
			folder:     "INBOX",
			uid:        "123",
			defaultKey: "default",
			wantKey:    "msg:acc_1:INBOX:123",
		},
		{
			name:       "invalid input returns default",
			accountID:  "",
			folder:     "INBOX",
			uid:        "123",
			defaultKey: "default-key",
			wantKey:    "default-key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := NewMessageKeyBuilder().
				AccountID(tt.accountID).
				Folder(tt.folder).
				UID(tt.uid).
				BuildOrDefault(tt.defaultKey)

			if key != tt.wantKey {
				t.Errorf("BuildOrDefault() key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}

func TestContentHashKey(t *testing.T) {
	tests := []struct {
		name        string
		contentHash string
		wantKey     string
		wantErr     bool
	}{
		{
			name:        "valid hash",
			contentHash: "6d4205ca59800307",
			wantKey:     "msg:hash:6d4205ca59800307",
			wantErr:     false,
		},
		{
			name:        "empty hash",
			contentHash: "",
			wantKey:     "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := ContentHashKey(tt.contentHash)
			if (err != nil) != tt.wantErr {
				t.Errorf("ContentHashKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if key != tt.wantKey {
				t.Errorf("ContentHashKey() key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}

func TestContentHashKeySafe(t *testing.T) {
	tests := []struct {
		name        string
		contentHash string
		wantKey     string
	}{
		{
			name:        "valid hash",
			contentHash: "6d4205ca59800307",
			wantKey:     "msg:hash:6d4205ca59800307",
		},
		{
			name:        "empty hash returns empty",
			contentHash: "",
			wantKey:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := ContentHashKeySafe(tt.contentHash)
			if key != tt.wantKey {
				t.Errorf("ContentHashKeySafe() key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}

func TestMessageListKey(t *testing.T) {
	tests := []struct {
		name      string
		accountID string
		folder    string
		cursor    string
		limit     int
		sortBy    string
		sortOrder string
		wantKey   string
	}{
		{
			name:      "standard message list key",
			accountID: "acc_1",
			folder:    "INBOX",
			cursor:    "0",
			limit:     50,
			sortBy:    "date",
			sortOrder: "desc",
			wantKey:   "msglist:acc_1:INBOX:0:50:date:desc",
		},
		{
			name:      "empty folder defaults to INBOX",
			accountID: "acc_1",
			folder:    "",
			cursor:    "0",
			limit:     50,
			sortBy:    "date",
			sortOrder: "desc",
			wantKey:   "msglist:acc_1:INBOX:0:50:date:desc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := MessageListKey(tt.accountID, tt.folder, tt.cursor, tt.limit, tt.sortBy, tt.sortOrder)
			if key != tt.wantKey {
				t.Errorf("MessageListKey() key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}

func TestFolderKey(t *testing.T) {
	tests := []struct {
		name      string
		accountID string
		folder    string
		wantKey   string
		wantErr   bool
	}{
		{
			name:      "valid folder key",
			accountID: "acc_1",
			folder:    "INBOX",
			wantKey:   "fld:acc_1:INBOX",
			wantErr:   false,
		},
		{
			name:      "empty folder defaults to INBOX",
			accountID: "acc_1",
			folder:    "",
			wantKey:   "fld:acc_1:INBOX",
			wantErr:   false,
		},
		{
			name:      "invalid account ID",
			accountID: "invalid",
			folder:    "INBOX",
			wantKey:   "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := FolderKey(tt.accountID, tt.folder)
			if (err != nil) != tt.wantErr {
				t.Errorf("FolderKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if key != tt.wantKey {
				t.Errorf("FolderKey() key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}

func TestFolderListKey(t *testing.T) {
	tests := []struct {
		name      string
		accountID string
		wantKey   string
		wantErr   bool
	}{
		{
			name:      "valid folder list key",
			accountID: "acc_1",
			wantKey:   "fld:acc_1",
			wantErr:   false,
		},
		{
			name:      "invalid account ID",
			accountID: "invalid",
			wantKey:   "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := FolderListKey(tt.accountID)
			if (err != nil) != tt.wantErr {
				t.Errorf("FolderListKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if key != tt.wantKey {
				t.Errorf("FolderListKey() key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}

func TestAccountKey(t *testing.T) {
	tests := []struct {
		name      string
		accountID string
		wantKey   string
		wantErr   bool
	}{
		{
			name:      "valid account key",
			accountID: "acc_1",
			wantKey:   "acct:acc_1",
			wantErr:   false,
		},
		{
			name:      "empty account ID",
			accountID: "",
			wantKey:   "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := AccountKey(tt.accountID)
			if (err != nil) != tt.wantErr {
				t.Errorf("AccountKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if key != tt.wantKey {
				t.Errorf("AccountKey() key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}

func TestEnvelopeKey(t *testing.T) {
	tests := []struct {
		name      string
		accountID string
		folder    string
		uid       string
		wantKey   string
		wantErr   bool
	}{
		{
			name:      "valid envelope key",
			accountID: "acc_1",
			folder:    "INBOX",
			uid:       "123",
			wantKey:   "env:acc_1:INBOX:123",
			wantErr:   false,
		},
		{
			name:      "empty folder defaults to INBOX",
			accountID: "acc_1",
			folder:    "",
			uid:       "123",
			wantKey:   "env:acc_1:INBOX:123",
			wantErr:   false,
		},
		{
			name:      "empty UID",
			accountID: "acc_1",
			folder:    "INBOX",
			uid:       "",
			wantKey:   "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := EnvelopeKey(tt.accountID, tt.folder, tt.uid)
			if (err != nil) != tt.wantErr {
				t.Errorf("EnvelopeKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if key != tt.wantKey {
				t.Errorf("EnvelopeKey() key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}

func TestThreadKey(t *testing.T) {
	tests := []struct {
		name      string
		accountID string
		threadID  string
		wantKey   string
		wantErr   bool
	}{
		{
			name:      "valid thread key",
			accountID: "acc_1",
			threadID:  "thd_123",
			wantKey:   "thd:acc_1:thd_123",
			wantErr:   false,
		},
		{
			name:      "empty thread ID",
			accountID: "acc_1",
			threadID:  "",
			wantKey:   "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := ThreadKey(tt.accountID, tt.threadID)
			if (err != nil) != tt.wantErr {
				t.Errorf("ThreadKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if key != tt.wantKey {
				t.Errorf("ThreadKey() key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}

func TestSearchKey(t *testing.T) {
	tests := []struct {
		name      string
		queryHash string
		wantKey   string
	}{
		{
			name:      "valid search key",
			queryHash: "abc123",
			wantKey:   "srch:abc123",
		},
		{
			name:      "empty hash returns empty",
			queryHash: "",
			wantKey:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := SearchKey(tt.queryHash)
			if key != tt.wantKey {
				t.Errorf("SearchKey() key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}

func TestTokenBucketKey(t *testing.T) {
	tests := []struct {
		name      string
		accountID string
		wantKey   string
		wantErr   bool
	}{
		{
			name:      "valid token bucket key",
			accountID: "acc_1",
			wantKey:   "tkn:acc_1",
			wantErr:   false,
		},
		{
			name:      "empty account ID",
			accountID: "",
			wantKey:   "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := TokenBucketKey(tt.accountID)
			if (err != nil) != tt.wantErr {
				t.Errorf("TokenBucketKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if key != tt.wantKey {
				t.Errorf("TokenBucketKey() key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}

func TestAttachmentKey(t *testing.T) {
	tests := []struct {
		name         string
		attachmentID string
		wantKey      string
	}{
		{
			name:         "valid attachment key",
			attachmentID: "att_123",
			wantKey:      "att:att_123",
		},
		{
			name:         "empty ID returns empty",
			attachmentID: "",
			wantKey:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := AttachmentKey(tt.attachmentID)
			if key != tt.wantKey {
				t.Errorf("AttachmentKey() key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}

func TestParseMessageKey(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		wantAcc   string
		wantFld   string
		wantUID   string
		wantErr   bool
	}{
		{
			name:    "valid key parsing",
			key:     "msg:acc_1:INBOX:343218",
			wantAcc: "acc_1",
			wantFld: "INBOX",
			wantUID: "343218",
			wantErr: false,
		},
		{
			name:    "different folder",
			key:     "msg:acc_1:Sent:123",
			wantAcc: "acc_1",
			wantFld: "Sent",
			wantUID: "123",
			wantErr: false,
		},
		{
			name:    "invalid prefix",
			key:     "invalid:acc_1:INBOX:123",
			wantAcc: "",
			wantFld: "",
			wantUID: "",
			wantErr: true,
		},
		{
			name:    "wrong number of parts",
			key:     "msg:acc_1:INBOX",
			wantAcc: "",
			wantFld: "",
			wantUID: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acc, fld, uid, err := ParseMessageKey(tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseMessageKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if acc != tt.wantAcc {
				t.Errorf("ParseMessageKey() accountID = %v, want %v", acc, tt.wantAcc)
			}
			if fld != tt.wantFld {
				t.Errorf("ParseMessageKey() folder = %v, want %v", fld, tt.wantFld)
			}
			if uid != tt.wantUID {
				t.Errorf("ParseMessageKey() uid = %v, want %v", uid, tt.wantUID)
			}
		})
	}
}

func TestValidationFunctions(t *testing.T) {
	t.Run("ValidateAccountID", func(t *testing.T) {
		tests := []struct {
			name    string
			id      string
			wantErr bool
		}{
			{"valid", "acc_1", false},
			{"valid with numbers", "acc_123", false},
			{"empty", "", true},
			{"wrong prefix", "account_1", true},
			{"no prefix", "123", true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := ValidateAccountID(tt.id)
				if (err != nil) != tt.wantErr {
					t.Errorf("ValidateAccountID() error = %v, wantErr %v", err, tt.wantErr)
				}
			})
		}
	})

	t.Run("ValidateFolder", func(t *testing.T) {
		tests := []struct {
			name    string
			folder  string
			wantErr bool
		}{
			{"valid INBOX", "INBOX", false},
			{"valid Sent", "Sent", false},
			{"valid with spaces", "My Folder", false},
			{"empty", "", true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := ValidateFolder(tt.folder)
				if (err != nil) != tt.wantErr {
					t.Errorf("ValidateFolder() error = %v, wantErr %v", err, tt.wantErr)
				}
			})
		}
	})

	t.Run("ValidateUID", func(t *testing.T) {
		tests := []struct {
			name    string
			uid     string
			wantErr bool
		}{
			{"valid", "123", false},
			{"valid large", "343218", false},
			{"empty", "", true},
			{"non-numeric", "abc", true},
			{"zero", "0", true},
			{"negative", "-1", true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := ValidateUID(tt.uid)
				if (err != nil) != tt.wantErr {
					t.Errorf("ValidateUID() error = %v, wantErr %v", err, tt.wantErr)
				}
			})
		}
	})
}
