package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeThunderbirdFixture(t *testing.T, profile, messages string) {
	t.Helper()
	mailDir := filepath.Join(profile, "ImapMail", "imap.gmail.com", "[Gmail].sbd")
	if err := os.MkdirAll(mailDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mailDir, "All Mail"), []byte(messages), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "prefs.js"), []byte(`user_pref("mail.account.account1.identities", "id1");
user_pref("mail.account.account1.server", "server1");
user_pref("mail.identity.id1.useremail", "fixture@example.test");
user_pref("mail.server.server1.directory-rel", "[ProfD]ImapMail/imap.gmail.com");
`), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestThunderbirdCodeFreshExactRecipientAndSender(t *testing.T) {
	profile := t.TempDir()
	writeThunderbirdFixture(t, profile, "From sender@example\nDate: Sat, 29 Aug 2026 10:00:00 +0000\nSubject: Verification code\nFrom: noreply@tm1.openai.com\nTo: fixture@example.test\nContent-Type: text/plain; charset=UTF-8\nContent-Transfer-Encoding: quoted-printable\n\nYour verification code is 123456=\n.\n")
	old := thunderbirdProfileRoot
	thunderbirdProfileRoot = profile
	t.Cleanup(func() { thunderbirdProfileRoot = old })
	code, err := detectThunderbirdCode("fixture@example.test", time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC))
	if err != nil || code.Code != "123456" {
		t.Fatalf("code=%+v err=%v", code, err)
	}
}

func TestThunderbirdCodeAcceptsIdenticalMailboxCopies(t *testing.T) {
	profile := t.TempDir()
	message := "From sender@example\nDate: Sat, 29 Aug 2026 10:00:00 +0000\nSubject: Verification code\nFrom: noreply@tm.openai.com\nTo: fixture@example.test\n\nYour verification code is 123456\n"
	writeThunderbirdFixture(t, profile, message)
	mailDir := filepath.Join(profile, "ImapMail", "imap.gmail.com", "[Gmail].sbd")
	if err := os.WriteFile(filepath.Join(mailDir, "INBOX"), []byte(message), 0600); err != nil {
		t.Fatal(err)
	}
	old := thunderbirdProfileRoot
	thunderbirdProfileRoot = profile
	t.Cleanup(func() { thunderbirdProfileRoot = old })
	code, err := detectThunderbirdCode("fixture@example.test", time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC))
	if err != nil || code.Code != "123456" {
		t.Fatalf("identical mailbox copies must resolve to one code: code=%+v err=%v", code, err)
	}
}

func TestThunderbirdCodeRejectsStaleWrongAndAmbiguous(t *testing.T) {
	profile := t.TempDir()
	writeThunderbirdFixture(t, profile, "From sender@example\nDate: Sat, 29 Aug 2026 10:00:00 +0000\nSubject: Verification code\nFrom: noreply@tm.openai.com\nTo: other@example.test\n\nYour verification code is 111111\n")
	old := thunderbirdProfileRoot
	thunderbirdProfileRoot = profile
	t.Cleanup(func() { thunderbirdProfileRoot = old })
	if _, err := detectThunderbirdCode("fixture@example.test", time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("wrong recipient must be rejected")
	}
	if _, err := detectThunderbirdCode("other@example.test", time.Date(2026, 8, 29, 11, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("stale code must be rejected")
	}
	writeThunderbirdFixture(t, profile, "From sender@example\nDate: Sat, 29 Aug 2026 12:00:00 +0000\nSubject: Verification code\nFrom: noreply@tm.openai.com\nTo: fixture@example.test\n\nYour verification code is 111111\n\nFrom sender@example\nDate: Sat, 29 Aug 2026 12:00:00 +0000\nSubject: Verification code\nFrom: noreply@tm.openai.com\nTo: fixture@example.test\n\nYour verification code is 222222\n")
	if _, err := detectThunderbirdCode("fixture@example.test", time.Date(2026, 8, 29, 11, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("same-time newest messages must be ambiguous")
	}
}
