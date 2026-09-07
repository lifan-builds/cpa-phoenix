package main

// Thunderbird integration is deliberately read-only.  This file discovers
// the active profile, walks mailbox files only, and keeps every parsed code in
// memory for the duration of one request.

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"mime/quotedprintable"
)

var (
	thunderbirdProfileRoot = ""
)

var verificationCodePattern = regexp.MustCompile(`(?i)(?:verification\s+code|one[- ]time\s+code|security\s+code|code)[:：]?[^0-9]{0,100}(\d{6})`)
var thunderbirdHTMLBlockPattern = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>|<style[^>]*>.*?</style>`)
var thunderbirdHTMLTagPattern = regexp.MustCompile(`(?s)<[^>]+>`)

var trustedThunderbirdSenders = map[string]struct{}{
	"noreply@tm.openai.com":   {},
	"noreply@tm1.openai.com":  {},
	"noreply@openai.com":      {},
	"no-reply@openai.com":     {},
	"do-not-reply@openai.com": {},
}

type thunderbirdCode struct {
	Code       string
	ReceivedAt time.Time
	Recipient  string
}

type thunderbirdCandidate struct {
	Code       string
	ReceivedAt time.Time
	Recipient  string
	Recipients []string
}

func discoverThunderbirdProfile() (string, error) {
	if p := strings.TrimSpace(firstNonEmpty(thunderbirdProfileRoot, os.Getenv("THUNDERBIRD_PROFILE"))); p != "" {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return filepath.Clean(p), nil
		}
		return "", errors.New("thunderbird_profile_missing")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		if u, uerr := user.Current(); uerr == nil {
			home = u.HomeDir
		}
	}
	if strings.TrimSpace(home) == "" {
		return "", errors.New("thunderbird_profile_missing")
	}
	iniPath := filepath.Join(home, "Library", "Thunderbird", "profiles.ini")
	raw, err := os.ReadFile(iniPath)
	if err != nil {
		return "", errors.New("thunderbird_profile_missing")
	}
	base := filepath.Dir(iniPath)
	type profileEntry struct {
		path           string
		relative       bool
		defaultProfile bool
	}
	profiles := make([]profileEntry, 0)
	var current *profileEntry
	installDefault := ""
	section := ""
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if current != nil && current.path != "" {
				profiles = append(profiles, *current)
			}
			section = strings.Trim(line, "[]")
			if strings.HasPrefix(section, "Profile") {
				current = &profileEntry{relative: true}
			} else {
				current = nil
			}
			continue
		}
		if current == nil {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 && section != "" && strings.HasPrefix(section, "Install") && strings.TrimSpace(parts[0]) == "Default" {
				installDefault = strings.TrimSpace(parts[1])
			}
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch strings.TrimSpace(parts[0]) {
		case "Path":
			current.path = strings.TrimSpace(parts[1])
		case "IsRelative":
			current.relative = strings.TrimSpace(parts[1]) != "0"
		case "Default":
			current.defaultProfile = strings.TrimSpace(parts[1]) == "1"
		}
	}
	if current != nil && current.path != "" {
		profiles = append(profiles, *current)
	}
	if len(profiles) == 0 {
		return "", errors.New("thunderbird_profile_missing")
	}
	selected := profiles[0]
	// The installation section is the authoritative active profile on macOS;
	// Thunderbird can leave an older Default=1 profile entry behind.
	if installDefault != "" {
		candidate := installDefault
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(base, filepath.FromSlash(candidate))
		}
		if candidateInfo, candidateErr := os.Stat(candidate); candidateErr == nil && candidateInfo.IsDir() {
			selected.path, selected.relative = candidate, false
		} else {
			for _, profile := range profiles {
				if profile.defaultProfile {
					selected = profile
					break
				}
			}
		}
	} else {
		for _, profile := range profiles {
			if profile.defaultProfile {
				selected = profile
				break
			}
		}
	}
	path := selected.path
	if selected.relative {
		path = filepath.Join(base, filepath.FromSlash(path))
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		// Some Thunderbird versions leave a stale Default=1 entry while the
		// locked installation points at the active profile. Prefer that path,
		// then fall back to the first existing profile.
		if installDefault != "" {
			candidate := installDefault
			if !filepath.IsAbs(candidate) {
				candidate = filepath.Join(base, filepath.FromSlash(candidate))
			}
			if candidateInfo, candidateErr := os.Stat(candidate); candidateErr == nil && candidateInfo.IsDir() {
				return filepath.Clean(candidate), nil
			}
		}
		for _, profile := range profiles {
			candidate := profile.path
			if profile.relative {
				candidate = filepath.Join(base, filepath.FromSlash(candidate))
			}
			if candidateInfo, candidateErr := os.Stat(candidate); candidateErr == nil && candidateInfo.IsDir() {
				return filepath.Clean(candidate), nil
			}
		}
		return "", errors.New("thunderbird_profile_missing")
	}
	return path, nil
}

// discoverThunderbirdMailboxes reads only prefs.js metadata.  It is useful
// for diagnostics and tests; message discovery remains constrained to Mail/
// ImapMail directories and never touches credential databases.
func discoverThunderbirdMailboxes(profile string) map[string][]string {
	result := make(map[string][]string)
	raw, err := os.ReadFile(filepath.Join(profile, "prefs.js"))
	if err != nil {
		return result
	}
	identityEmail := make(map[string]string)
	accountIdentity := make(map[string]string)
	accountServer := make(map[string]string)
	serverDir := make(map[string]string)
	userPref := regexp.MustCompile(`user_pref\("([^"]+)",\s*"((?:\\.|[^"\\])*)"\);`)
	for _, m := range userPref.FindAllStringSubmatch(string(raw), -1) {
		key, value := m[1], m[2]
		value = strings.ReplaceAll(value, `\"`, `"`)
		switch {
		case strings.Contains(key, ".identities"):
			// Identities are often a comma-separated list; resolve each below.
			account := strings.TrimSuffix(strings.TrimPrefix(key, "mail.account."), ".identities")
			for _, id := range strings.Split(value, ",") {
				id = strings.TrimSpace(id)
				if id != "" {
					accountIdentity[account] = id
					break
				}
			}
		case strings.HasPrefix(key, "mail.identity.") && strings.HasSuffix(key, ".useremail"):
			identityEmail[strings.TrimSuffix(strings.TrimPrefix(key, "mail.identity."), ".useremail")] = value
		case strings.HasPrefix(key, "mail.account.") && strings.HasSuffix(key, ".server"):
			accountServer[strings.TrimSuffix(strings.TrimPrefix(key, "mail.account."), ".server")] = value
		case strings.HasPrefix(key, "mail.server.") && strings.HasSuffix(key, ".directory-rel"):
			serverDir[strings.TrimSuffix(strings.TrimPrefix(key, "mail.server."), ".directory-rel")] = value
		case strings.HasPrefix(key, "mail.server.") && strings.HasSuffix(key, ".directory"):
			serverDir[strings.TrimSuffix(strings.TrimPrefix(key, "mail.server."), ".directory")] = value
		}
	}
	for account, server := range accountServer {
		dir := serverDir[server]
		if dir == "" {
			continue
		}
		if strings.HasPrefix(dir, "[ProfD]") {
			dir = filepath.Join(profile, strings.TrimPrefix(dir, "[ProfD]"))
		} else if !filepath.IsAbs(dir) {
			dir = filepath.Join(profile, dir)
		}
		email := identityEmail[accountIdentity[account]]
		result[strings.ToLower(strings.TrimSpace(email))] = append(result[strings.ToLower(strings.TrimSpace(email))], filepath.Clean(dir))
	}
	return result
}

func mailboxFilesForRecipient(profile, recipient string) []string {
	mailboxes := discoverThunderbirdMailboxes(profile)
	dirs := mailboxes[strings.ToLower(strings.TrimSpace(recipient))]
	if len(dirs) == 0 {
		// Without an exact prefs.js account mapping, scanning every mailbox
		// would be both needlessly expensive and unsafe for cross-account mail.
		return nil
	}
	seen := make(map[string]bool)
	var files []string
	for _, dir := range dirs {
		for _, path := range mailboxFilesUnder(dir, dir) {
			if !seen[path] {
				seen[path] = true
				files = append(files, path)
			}
		}
	}
	sort.Strings(files)
	return files
}

func mailboxFilesUnder(root, profile string) []string {
	if profile == "" {
		profile = root
	}
	seen := make(map[string]bool)
	var files []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := strings.ToLower(d.Name())
		if d.IsDir() {
			if name == "mail" || name == "imapmail" || strings.HasSuffix(name, ".sbd") {
				return nil
			}
			return nil
		}
		if !d.Type().IsRegular() || name == "prefs.js" || name == "profiles.ini" || strings.Contains(name, "key4") || strings.Contains(name, "logins") || strings.HasSuffix(name, ".msf") || strings.HasSuffix(name, ".dat") || strings.HasSuffix(name, ".sqlite") {
			return nil
		}
		underMailbox := filepath.Dir(path) == root
		for p := filepath.Dir(path); p != profile && p != filepath.Dir(p); p = filepath.Dir(p) {
			part := strings.ToLower(filepath.Base(p))
			if part == "mail" || part == "imapmail" || strings.HasSuffix(part, ".sbd") {
				underMailbox = true
				break
			}
		}
		if underMailbox && !seen[path] {
			baseName := strings.ToLower(filepath.Base(path))
			if baseName != "inbox" && baseName != "all mail" {
				return nil
			}
			seen[path] = true
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files
}

func decodeThunderbirdPart(raw []byte, transfer string) []byte {
	if strings.EqualFold(strings.TrimSpace(transfer), "quoted-printable") {
		if decoded, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(raw))); err == nil {
			return decoded
		}
	}
	if strings.EqualFold(strings.TrimSpace(transfer), "base64") {
		if decoded, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, strings.NewReader(strings.Join(strings.Fields(string(raw)), "")))); err == nil {
			return decoded
		}
	}
	return raw
}

func thunderbirdTextFromMessage(header mail.Header, body []byte) string {
	contentType := header.Get("Content-Type")
	mediaType, params, _ := mime.ParseMediaType(contentType)
	decoded := decodeThunderbirdPart(body, header.Get("Content-Transfer-Encoding"))
	if strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		mr := multipart.NewReader(bytes.NewReader(decoded), params["boundary"])
		var out strings.Builder
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				break
			}
			partBody, _ := io.ReadAll(io.LimitReader(part, 1<<20))
			partHeader := mail.Header(part.Header)
			text := thunderbirdTextFromMessage(partHeader, partBody)
			if strings.EqualFold(strings.Split(partHeader.Get("Content-Type"), ";")[0], "text/html") {
				text = stripThunderbirdHTML(text)
			}
			out.WriteString(text)
			out.WriteByte('\n')
		}
		return out.String()
	}
	if strings.EqualFold(strings.Split(contentType, ";")[0], "text/html") || strings.EqualFold(mediaType, "text/html") {
		return stripThunderbirdHTML(string(decoded))
	}
	return string(decoded)
}

func stripThunderbirdHTML(value string) string {
	value = thunderbirdHTMLBlockPattern.ReplaceAllString(value, " ")
	value = thunderbirdHTMLTagPattern.ReplaceAllString(value, " ")
	value = strings.ReplaceAll(value, "&nbsp;", " ")
	value = strings.ReplaceAll(value, "&amp;", "&")
	return value
}

func parseThunderbirdMessage(raw []byte) (thunderbirdCandidate, bool) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return thunderbirdCandidate{}, false
	}
	from, err := msg.Header.AddressList("From")
	if err != nil || len(from) != 1 {
		return thunderbirdCandidate{}, false
	}
	sender := strings.ToLower(strings.TrimSpace(from[0].Address))
	if _, ok := trustedThunderbirdSenders[sender]; !ok {
		return thunderbirdCandidate{}, false
	}
	to, err := msg.Header.AddressList("To")
	if err != nil || len(to) == 0 {
		return thunderbirdCandidate{}, false
	}
	recipients := make([]string, 0, len(to))
	for _, address := range to {
		recipients = append(recipients, strings.ToLower(strings.TrimSpace(address.Address)))
	}
	date, err := mail.ParseDate(msg.Header.Get("Date"))
	if err != nil {
		return thunderbirdCandidate{}, false
	}
	body, _ := io.ReadAll(io.LimitReader(msg.Body, 2<<20))
	text := thunderbirdTextFromMessage(msg.Header, body)
	text = strings.Join(strings.Fields(text), " ")
	subject := strings.ToLower(strings.TrimSpace(msg.Header.Get("Subject")))
	if !strings.Contains(subject, "verification") && !strings.Contains(subject, "code") {
		return thunderbirdCandidate{}, false
	}
	match := verificationCodePattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return thunderbirdCandidate{}, false
	}
	return thunderbirdCandidate{Code: match[len(match)-1], ReceivedAt: date, Recipient: recipients[0], Recipients: recipients}, true
}

func readThunderbirdMailbox(path string) [][]byte {
	const maxMailboxRead = 16 << 20
	info, statErr := os.Stat(path)
	if statErr != nil || !info.Mode().IsRegular() {
		return nil
	}
	var raw []byte
	var err error
	if info.Size() > maxMailboxRead {
		file, openErr := os.Open(path)
		if openErr != nil {
			return nil
		}
		_, _ = file.Seek(-maxMailboxRead, io.SeekEnd)
		raw, err = io.ReadAll(io.LimitReader(file, maxMailboxRead))
		_ = file.Close()
		// Drop the partial first message; all complete messages after the next
		// mbox delimiter are newer and are sufficient for fresh-code lookup.
		if idx := bytes.Index(raw, []byte("\nFrom ")); idx >= 0 {
			raw = raw[idx+1:]
		}
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil || len(raw) == 0 {
		return nil
	}
	// Thunderbird mbox files delimit messages with a line beginning "From ".
	// If no delimiter exists, treat the file as one RFC822 message.
	lines := bytes.Split(raw, []byte("\n"))
	var chunks [][]byte
	start := 0
	if len(lines) > 0 && bytes.HasPrefix(lines[0], []byte("From ")) {
		start = 1
	}
	for i, line := range lines {
		if i > 0 && bytes.HasPrefix(line, []byte("From ")) {
			if part := bytes.Join(lines[start:i], []byte("\n")); len(bytes.TrimSpace(part)) > 0 {
				chunks = append(chunks, part)
			}
			start = i + 1
		}
	}
	if part := bytes.Join(lines[start:], []byte("\n")); len(bytes.TrimSpace(part)) > 0 {
		chunks = append(chunks, part)
	}
	return chunks
}

func detectThunderbirdCode(recipient string, requestedAt time.Time) (thunderbirdCode, error) {
	profile, err := discoverThunderbirdProfile()
	if err != nil {
		return thunderbirdCode{}, err
	}
	recipient = strings.ToLower(strings.TrimSpace(recipient))
	if recipient == "" {
		return thunderbirdCode{}, errors.New("verification_recipient_missing")
	}
	var candidates []thunderbirdCandidate
	for _, path := range mailboxFilesForRecipient(profile, recipient) {
		for _, raw := range readThunderbirdMailbox(path) {
			candidate, ok := parseThunderbirdMessage(raw)
			if !ok || candidate.ReceivedAt.IsZero() || !candidate.ReceivedAt.After(requestedAt) || len(candidate.Recipients) != 1 || !strings.EqualFold(candidate.Recipient, recipient) {
				continue
			}
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		return thunderbirdCode{}, errors.New("verification_code_not_found")
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].ReceivedAt.After(candidates[j].ReceivedAt) })
	latest := candidates[0]
	for _, candidate := range candidates {
		if candidate.ReceivedAt.Equal(latest.ReceivedAt) {
			if candidate.Code != latest.Code {
				return thunderbirdCode{}, errors.New("verification_code_ambiguous")
			}
		}
	}
	return thunderbirdCode{Code: latest.Code, ReceivedAt: latest.ReceivedAt, Recipient: recipient}, nil
}
