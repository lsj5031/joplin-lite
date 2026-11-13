package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/lsj5031/joplin-lite/internal/assistant"
)

const askCitationScheme = "ask-cite:"

func askCitationKey(citation assistant.Citation) string {
	return citation.NoteID + "\x00" + citation.Version
}

func (a *nativeApp) askMarkdownOptions() *markdown.Options {
	if a.ask.md == nil {
		a.ask.md = &markdown.Options{}
	}
	if a.ask.mdTheme == nil {
		a.ask.mdTheme = &markdown.Theme{
			FontSize:       sizeUI,
			LineHeight:     1.5,
			Text:           inkNavy,
			Muted:          inkSoft,
			Link:           routeBlue,
			Strong:         inkNavy,
			Code:           inkNavy,
			CodeBackground: paperSunken,
			QuoteBar:       hairline,
			QuoteText:      inkSoft,
			Rule:           hairline,
		}
	}
	a.ask.md.Theme = a.ask.mdTheme
	a.ask.md.OnLink = a.openAskMarkdownLink
	return a.ask.md
}

// askCitationMarkdown turns only verified citation tokens into links. Unknown
// tokens remain visible as plain text, so a model cannot invent a clickable
// source by writing [E99].
func askCitationMarkdown(text string, citations []assistant.Citation, ref string) string {
	for _, citation := range citations {
		if citation.Token == "" {
			continue
		}
		text = strings.ReplaceAll(text,
			"["+citation.Token+"]",
			"[["+citation.Token+"]](ask-cite:"+ref+":"+citation.Token+")",
		)
	}
	return text
}

func (a *nativeApp) askAnswer(c *ui.Context, text string, citations []assistant.Citation, ref string, width float32) {
	source := askCitationMarkdown(text, citations, ref)
	ui.Column(c).Width(width).Children(func() {
		markdown.View(c, source, a.askMarkdownOptions()).MaxWidth(width)
	})
}

func (a *nativeApp) askCitationStale(citation assistant.Citation) bool {
	current, known := a.ask.citationStatus[askCitationKey(citation)]
	return known && !current
}

func (a *nativeApp) askCitationButtons(c *ui.Context, citations []assistant.Citation, width float32) {
	if len(citations) == 0 {
		return
	}
	ui.Text(c, "Sources").FontSize(sizeSmall).Bold().TextColor(inkSoft)
	stale := 0
	for _, citation := range citations {
		label := fmt.Sprintf("[%s] %s", citation.Token, clip(citation.Title, 34))
		if a.askCitationStale(citation) {
			label += " · changed"
			stale++
		}
		if ui.Button(c, label).Tooltip(citation.Excerpt).Clicked() {
			a.openAskCitation(citation)
		}
	}
	if stale > 0 {
		ui.Text(c, "A cited note changed since this answer. Open it to review the current text; the saved excerpt is shown on the source.").
			FontSize(sizeSmall).TextColor(amberInk).MaxWidth(width)
	}
}

func (a *nativeApp) openAskMarkdownLink(link string) {
	rest, ok := strings.CutPrefix(link, askCitationScheme)
	if !ok {
		a.openLink(link)
		return
	}
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 {
		return
	}
	ref, token := parts[0], parts[1]
	var citations []assistant.Citation
	if ref == "live" {
		citations = a.ask.citations
	} else if index, err := strconv.Atoi(ref); err == nil && index >= 0 && index < len(a.ask.messages) {
		citations = a.ask.messages[index].Citations
	}
	for _, citation := range citations {
		if citation.Token == token {
			a.openAskCitation(citation)
			return
		}
	}
}

// History opens immediately for this frequent keyboard action. Only its chat
// list scrolls; management actions stay within the window below it.
func (a *nativeApp) askHistoryMenu(c *ui.Context, anchor ui.Element, width float32) {
	menuWidth := min(300, width-8)
	_, height := c.Size()
	ui.PopoverBase(c, anchor, &a.ask.historyOpen, func(panel ui.Element) {
		panelStyle(panel, menuWidth).Padding(6).Gap(2)
		label(c, "Chats")
		if a.ask.historyBusy {
			ui.Text(c, "Loading…").FontSize(sizeSmall).TextColor(inkSoft).Padding(4, 6)
			return
		}
		ui.Scroll(c.Key("ask-history-list")).TrackScroll(&a.ask.historyScroll).
			MaxHeight(min(280, max(80, height-360))).Shrink(1).Children(func() {
			for _, conversation := range a.ask.conversations {
				title := conversation.Title
				if strings.TrimSpace(title) == "" {
					title = "New chat"
				}
				chosen := conversation.ConversationID == a.ask.conversationID
				row := menuRow(c, "ask-chat:"+conversation.ConversationID, chosen).Tooltip(title)
				row.Children(func() {
					ui.Text(c, title).MaxWidth(menuWidth - 46).Grow(1)
					if chosen {
						ui.Text(c, "✓").TextColor(violet)
					}
				})
				if row.Clicked() {
					a.selectAskChat(conversation.ConversationID)
				}
				if conversation.Preview != "" {
					ui.Text(c, clip(strings.Join(strings.Fields(conversation.Preview), " "), 54)).
						FontSize(sizeSmall).TextColor(inkSoft).SingleLine().Padding(0, 10, 0, 6)
				}
			}
		})
		ui.Divider(c)
		if menuItem(c, "ask-chat:new", "New chat", false) {
			a.newAskChat("Started a new chat.")
		}
		if a.ask.historyErr != "" {
			ui.Text(c, a.ask.historyErr).FontSize(sizeSmall).TextColor(signalRed).MaxWidth(menuWidth - 12)
		}
		if a.ask.renameID != "" {
			ui.Text(c, "Rename chat").FontSize(sizeSmall).Bold().TextColor(inkSoft).Padding(4, 6, 0, 6)
			ui.TextInput(c.Key("ask-rename-title"), &a.ask.renameText).
				Label("Chat title").Width(menuWidth - 12)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Cancel").Clicked() {
					a.ask.renameID = ""
					a.ask.renameText = ""
				}
				if primaryAction(c, "Save title").Clicked() {
					a.renameAskChat()
				}
			})
		} else if menuItem(c, "ask-chat:rename", "Rename current chat…", false) {
			a.ask.renameID = a.ask.conversationID
			a.ask.renameText = a.currentAskTitle()
			a.ask.deleteID = ""
		}
		if a.ask.deleteID != "" {
			ui.Text(c, "Delete this chat?").FontSize(sizeSmall).Bold().TextColor(signalRed).Padding(4, 6, 0, 6)
			ui.Text(c, "Its messages and saved citations will be removed. This cannot be undone.").
				FontSize(sizeSmall).TextColor(inkSoft).MaxWidth(menuWidth - 12)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c.Key("ask-delete-cancel"), "Cancel").AutoFocus().Clicked() {
					a.ask.deleteID = ""
				}
				if underTheme(c, dangerTheme, func() ui.Element { return ui.PrimaryButton(c, "Delete chat") }).Clicked() {
					a.deleteAskChat()
				}
			})
		} else if menuItem(c, "ask-chat:delete", "Delete current chat…", false) {
			a.ask.deleteID = a.ask.conversationID
			a.ask.renameID = ""
		}
	})
}
