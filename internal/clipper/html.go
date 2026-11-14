package clipper

import (
	"strings"

	htmltomd "github.com/JohannesKaufmann/html-to-markdown/v2"
)

// htmlToMarkdown converts HTML content to Markdown.
func htmlToMarkdown(html string) (string, error) {
	md, err := htmltomd.ConvertString(html)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(md), nil
}

// prependDataImage keeps a clipped image inline. The clipper sends images whose
// URLs are gated as data URLs; embedding the bytes is the only copy that
// survives, and a screenshot is the note's first content.
func prependDataImage(body, dataURL string) string {
	if !strings.HasPrefix(dataURL, "data:image/") || !strings.Contains(dataURL, ",") {
		return body
	}
	image := "![Clipped image](" + dataURL + ")"
	if strings.TrimSpace(body) == "" {
		return image
	}
	return image + "\n\n" + body
}
