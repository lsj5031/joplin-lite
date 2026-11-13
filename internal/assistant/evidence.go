package assistant

import (
	"regexp"
	"strings"
	"sync"
)

var citationToken = regexp.MustCompile(`\[E([0-9]{1,4})\]`)

// evidenceRegistry holds the exact passages read through read_note. Search
// snippets are not evidence. A citation token resolves only through this map.
type evidenceRegistry struct {
	mu     sync.Mutex
	items  []Evidence
	tokens map[string]int
}

type Evidence struct {
	Token       string
	NoteID      string
	Title       string
	Version     string
	StartByte   int
	EndByte     int
	Text        string
	UpdatedTime int64
}

func newEvidenceRegistry() *evidenceRegistry {
	return &evidenceRegistry{tokens: make(map[string]int)}
}

func (r *evidenceRegistry) add(e Evidence) Evidence {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Re-reading the same range of the same version reuses its token, so a
	// paginated note does not grow the registry with duplicate evidence.
	key := e.NoteID + "\x00" + e.Version + "\x00" + itoa(e.StartByte) + "\x00" + itoa(e.EndByte)
	if i, ok := r.tokens[key]; ok {
		return r.items[i]
	}
	e.Token = "E" + itoa(len(r.items)+1)
	r.tokens[key] = len(r.items)
	r.items = append(r.items, e)
	return e
}

// cite resolves only tokens the agent actually read. Unknown tokens are left
// in the answer but never become clickable.
func (r *evidenceRegistry) cite(answer string) []Citation {
	r.mu.Lock()
	byToken := make(map[string]Evidence, len(r.items))
	for _, item := range r.items {
		byToken[item.Token] = item
	}
	r.mu.Unlock()

	seen := make(map[string]bool)
	var out []Citation
	for _, match := range citationToken.FindAllStringSubmatch(answer, -1) {
		token := "E" + match[1]
		if seen[token] {
			continue
		}
		item, ok := byToken[token]
		if !ok {
			continue
		}
		seen[token] = true
		out = append(out, Citation{
			Token:    token,
			NoteID:   item.NoteID,
			Title:    item.Title,
			Excerpt:  excerpt(item.Text, 1200),
			Version:  item.Version,
			Verified: true,
		})
	}
	return out
}

func excerpt(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "…"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
