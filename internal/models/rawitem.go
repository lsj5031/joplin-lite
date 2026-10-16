package models

import (
	"fmt"
	"strings"
)

// MetaLine is one raw "key: value" metadata line from a serialized item.
type MetaLine struct {
	Key      string // parsed key (trimmed)
	Value    string // decoded value (Joplin's unserialize_format escaping reversed)
	RawValue string // value text as written (trimmed, still escaped)
	Raw      string // the exact source line, byte for byte
}

// RawItem is a lossless parse of Joplin's serialized item text. It mirrors
// Joplin's BaseItem.unserialize layout:
//
//	<title line>
//	<blank>
//	<body lines>        (omitted entirely when the body is empty)
//	<blank>
//	<key: value lines>  (metadata; "type_" last in Joplin's writer)
//
// Bytes() re-emits the exact source bytes; SerializeWithOverrides re-emits a
// changed item with unknown metadata lines kept verbatim in their original
// positions. This is the losslessness layer behind the sync push path.
type RawItem struct {
	Type       int        // parsed type_ value
	HasHead    bool       // a separator blank line existed (title/body section present)
	Head       string     // exact source text before the separator line
	Sep        string     // the exact separator line ("" when cleanly blank)
	Title      string     // first head line (as parsed)
	Body       string     // head lines after title + its blank, joined with \n
	Meta       []MetaLine // metadata lines in original order
	TrailingNL string     // exact trailing newline bytes after the last meta line
}

// ParseItem parses content the way Joplin's BaseItem.unserialize does (same
// scan-from-the-end metadata detection, same value trimming), while keeping
// every raw byte needed for exact re-emission.
func ParseItem(content string) (*RawItem, error) {
	r := &RawItem{}
	// Joplin's writer emits no trailing newline; tolerate and remember one.
	for strings.HasSuffix(content, "\n") || strings.HasSuffix(content, "\r") {
		r.TrailingNL = content[len(content)-1:] + r.TrailingNL
		content = content[:len(content)-1]
	}
	if content == "" {
		return r, nil
	}

	lines := strings.Split(content, "\n")

	// Scan from the end: metadata is the run of non-blank lines at the tail;
	// the first blank line walking backwards separates the head section.
	sep := -1
	for i := len(lines) - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			sep = i
			break
		}
		if !strings.Contains(t, ":") {
			return nil, fmt.Errorf("invalid property format: %q", lines[i])
		}
	}

	metaStart := sep + 1
	if sep < 0 {
		metaStart = 0
	} else {
		r.HasHead = true
		r.Sep = lines[sep]
		r.Head = strings.Join(lines[:sep], "\n")
	}

	for _, line := range lines[metaStart:] {
		t := strings.TrimSpace(line)
		p := strings.Index(t, ":")
		if p < 0 {
			return nil, fmt.Errorf("invalid property format: %q", line)
		}
		key := strings.TrimSpace(t[:p])
		rawValue := strings.TrimSpace(t[p+1:])
		r.Meta = append(r.Meta, MetaLine{
			Key:      key,
			Value:    unescapeMetaValue(key, rawValue),
			RawValue: rawValue,
			Raw:      line,
		})
	}

	if r.HasHead {
		headLines := strings.Split(r.Head, "\n")
		r.Title = headLines[0]
		if len(headLines) > 2 {
			r.Body = strings.Join(headLines[2:], "\n")
		}
	}

	for _, ml := range r.Meta {
		if ml.Key == "type_" {
			fmt.Sscanf(ml.Value, "%d", &r.Type)
		}
	}
	if r.Type == 0 {
		return nil, fmt.Errorf("missing required property: type_")
	}
	return r, nil
}

// Bytes re-emits the exact source bytes.
func (r *RawItem) Bytes() string {
	var b strings.Builder
	if r.HasHead {
		b.WriteString(r.Head)
		b.WriteString("\n")
		b.WriteString(r.Sep)
		b.WriteString("\n")
	}
	for i, ml := range r.Meta {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(ml.Raw)
	}
	b.WriteString(r.TrailingNL)
	return b.String()
}

// SerializeWithOverrides re-emits the item with the given formatted field
// values (key -> Joplin-serialized value text, unescaped). A metadata line is
// kept byte-verbatim when its decoded value equals the override; unknown keys
// (no override) are kept verbatim in their original position; overridden keys
// absent from the source are appended at the end. The head section is rebuilt
// from "title"/"body" overrides when they differ from the parsed values.
func (r *RawItem) SerializeWithOverrides(over map[string]string) string {
	out := &RawItem{
		Type:       r.Type,
		HasHead:    r.HasHead,
		Head:       r.Head,
		Sep:        r.Sep,
		TrailingNL: r.TrailingNL,
	}

	title, hasTitle := over["title"]
	body, hasBody := over["body"]
	if !hasTitle {
		title = r.Title
	}
	if !hasBody {
		body = r.Body
	}
	if hasTitle || hasBody {
		if title != r.Title || body != r.Body {
			// Rebuild canonically (Joplin's writer: body section only if non-empty).
			head := title
			if body != "" {
				head += "\n\n" + body
			}
			out.Head = head
			out.Sep = ""
			if head != "" {
				out.HasHead = true
			}
		}
	}

	seen := make(map[string]bool)
	for _, ml := range r.Meta {
		seen[ml.Key] = true
		v, ok := over[ml.Key]
		if !ok || v == ml.Value {
			out.Meta = append(out.Meta, ml) // verbatim: unknown key, or unchanged value
			continue
		}
		out.Meta = append(out.Meta, MetaLine{
			Key:      ml.Key,
			Value:    v,
			RawValue: escapeMetaValue(ml.Key, v),
			Raw:      ml.Key + ": " + escapeMetaValue(ml.Key, v),
		})
	}
	for _, k := range metadataKeyOrder {
		if seen[k] {
			continue
		}
		v, ok := over[k]
		if !ok {
			continue
		}
		out.Meta = append(out.Meta, MetaLine{
			Key:      k,
			Value:    v,
			RawValue: escapeMetaValue(k, v),
			Raw:      k + ": " + escapeMetaValue(k, v),
		})
	}
	return out.Bytes()
}
