package mimeparser

import (
	"strings"
	"testing"
)

func TestMIMEParser_DostowInvoice(t *testing.T) {
	rawEmail := `Return-Path: <hello@dostow.com>
Delivered-To: me@osiloke.com
Received: from mx.lin1.dostow.com
	by mx.lin1.dostow.com with LMTP
	id 6IJXKWUmsWmE5yEAGoaVnA
	(envelope-from <hello@dostow.com>)
	for <me@osiloke.com>; Wed, 11 Mar 2026 08:23:01 +0000
MIME-Version: 1.0
Date: Wed, 11 Mar 2026 08:23:01 +0000
From: hello@dostow.com
To: me@osiloke.com
Message-ID: <3t77aewr.1773217381465596580.xxo9@dostow.com>
Subject: Dostow | Invoice
X-Dostow-Trace-ID: ed55966a-d666-45cb-ad14-fd34883c0c98
Content-Type: multipart/alternative;
 boundary=ec23672ba45d7b3bb3813f3bbf0719c693aab1aa607249df37a88c2df8e5

--ec23672ba45d7b3bb3813f3bbf0719c693aab1aa607249df37a88c2df8e5
Content-Transfer-Encoding: quoted-printable
Content-Type: text/html; charset=UTF-8

<!DOCTYPE html><html><body><h1>Invoice</h1></body></html>
--ec23672ba45d7b3bb3813f3bbf0719c693aab1aa607249df37a88c2df8e5
Content-Transfer-Encoding: quoted-printable
Content-Type: text/x-amp-html; charset=UTF-8

<!DOCTYPE html><html><body><h1>Invoice AMP</h1></body></html>
--ec23672ba45d7b3bb3813f3bbf0719c693aab1aa607249df37a88c2df8e5--`

	// Ensure CRLF
	rawEmail = strings.ReplaceAll(rawEmail, "\n", "\r\n")
	rawEmail = strings.ReplaceAll(rawEmail, "\r\r\n", "\r\n")

	parser := NewMIMEParser("/tmp")
	
	config := ParseConfig{
		IncludeHeaders:     true,
		IncludeBody:        true,
		IncludeAttachments: true,
	}

	result, err := parser.Parse([]byte(rawEmail), config)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	msg := result.Message

	if msg.Subject != "Dostow | Invoice" {
		t.Errorf("Expected subject 'Dostow | Invoice', got %q", msg.Subject)
	}

	if msg.Date.IsZero() {
		t.Errorf("Expected non-zero date")
	}

	if msg.MessageID != "<3t77aewr.1773217381465596580.xxo9@dostow.com>" {
		t.Errorf("Expected message ID '<3t77aewr.1773217381465596580.xxo9@dostow.com>', got %q", msg.MessageID)
	}

	if len(msg.Headers) == 0 {
		t.Errorf("Expected headers to be parsed, got empty")
	}

	if msg.Body == nil {
		t.Fatalf("Expected body to be parsed")
	}

	t.Logf("Subject: %v", msg.Subject)
	t.Logf("Date: %v", msg.Date)
	t.Logf("HTML Body Length: %d", len(msg.Body.HTML))
	t.Logf("Text Body Length: %d", len(msg.Body.Text))
	t.Logf("Text Body: %q", msg.Body.Text)
}
