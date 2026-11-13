package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/lsj5031/joplin-lite/internal/assistant"
)

const (
	askPanelWidth = 400
	askMinWidth   = 320
	askMaxWidth   = 620
)

// askState is UI-thread state for the Ask panel. Assistant events arrive on a
// background goroutine; onAskEvent posts them here with mygo.RunOnMain.
type askState struct {
	open        bool
	width       float32
	input       string
	scope       int // 0 note, 1 notebook, 2 vault
	includeDesc bool

	busy     bool
	runID    string
	answer   string
	activity string
	err      string
	notice   string

	messages       []assistant.Message
	citations      []assistant.Citation
	citationStatus map[string]bool
	conversationID string
	conversations  []assistant.ConversationSummary

	historyOpen bool
	historyBusy bool
	historyErr  string
	renameID    string
	renameText  string
	deleteID    string

	settingsOpen bool
	settings     assistant.Settings
	key          string
	rememberKey  bool
	contextText  string
	maxText      string
	settingsErr  string
	testNotice   string
	testing      bool
	connection   string
	connected    bool

	md      *markdown.Options
	mdTheme *markdown.Theme
}

func newAskState() askState {
	s := assistant.DefaultSettings()
	return askState{
		width:          askPanelWidth,
		settings:       s,
		contextText:    strconv.Itoa(s.ContextWindow),
		maxText:        strconv.Itoa(s.MaxOutput),
		citationStatus: make(map[string]bool),
	}
}

func (a *nativeApp) toggleAsk() {
	a.ask.open = !a.ask.open
	if a.ask.open {
		a.refreshAsk()
	}
}

func (a *nativeApp) askManager() *assistant.Manager {
	if a.be == nil {
		return nil
	}
	return a.be.AskManager()
}

func (a *nativeApp) refreshAsk() {
	mgr := a.askManager()
	if mgr == nil {
		a.ask.err = "Ask is unavailable in this build."
		return
	}
	id := mgr.CurrentConversationID()
	snapshot := mgr.Snapshot(id)
	a.ask.conversationID = snapshot.ConversationID
	a.ask.messages = snapshot.Messages
	a.ask.conversations = mgr.ListConversations()
	a.ask.busy = snapshot.Running
	if !snapshot.Running {
		a.ask.runID = ""
	}
	a.ask.citationStatus = make(map[string]bool)
	for _, message := range snapshot.Messages {
		for _, citation := range message.Citations {
			current, err := mgr.CitationCurrent(citation)
			if err == nil {
				a.ask.citationStatus[askCitationKey(citation)] = current
			}
		}
	}
}

func (a *nativeApp) onAskEvent(event assistant.Event) {
	mygo.RunOnMain(func() {
		a.applyAskEvent(event)
	})
}

func (a *nativeApp) applyAskEvent(event assistant.Event) {
	if event.Type != "run.started" && event.RunID != "" {
		if a.ask.runID == "" || event.RunID != a.ask.runID {
			return // a late event from a previous run cannot overwrite this chat
		}
	}
	switch event.Type {
	case "run.started":
		a.ask.runID = event.RunID
		a.ask.busy = true
		a.ask.answer = ""
		a.ask.activity = "Starting"
		a.ask.err = ""
		a.ask.citations = nil
		a.refreshAsk()
	case "activity.updated":
		if event.Activity != nil && event.Activity.Active > 0 {
			a.ask.activity = event.Activity.Label
		} else {
			a.ask.activity = ""
		}
	case "answer.delta":
		a.ask.answer += event.Text
		a.ask.activity = ""
	case "answer.snapshot":
		if event.Text != "" || a.ask.answer == "" {
			a.ask.answer = event.Text
		}
	case "citation.resolved":
		if event.Citation != nil {
			found := false
			for _, citation := range a.ask.citations {
				if citation.Token == event.Citation.Token {
					found = true
					break
				}
			}
			if !found {
				a.ask.citations = append(a.ask.citations, *event.Citation)
			}
		}
	case "run.completed":
		a.ask.busy = false
		a.ask.activity = ""
		a.ask.runID = ""
		if len(event.Citations) > 0 {
			a.ask.citations = event.Citations
		}
		a.refreshAsk()
		a.ask.answer = ""
		a.ask.citations = nil
	case "run.cancelled":
		a.ask.busy = false
		a.ask.activity = ""
		a.ask.runID = ""
		a.ask.notice = "Stopped."
		a.refreshAsk()
		a.ask.answer = ""
	case "run.failed":
		a.ask.busy = false
		a.ask.activity = ""
		a.ask.runID = ""
		a.ask.err = event.Message
		a.refreshAsk()
		a.ask.answer = ""
	case "conversation.reset":
		a.ask.messages = nil
		a.ask.answer = ""
		a.ask.citations = nil
		a.ask.busy = false
		a.ask.activity = ""
	}
	if a.invalidate != nil {
		a.invalidate()
	}
}

func (a *nativeApp) askScope() (assistant.Scope, error) {
	switch a.ask.scope {
	case 0:
		if a.note == nil {
			return assistant.Scope{}, fmt.Errorf("open a note first, or choose another Ask scope")
		}
		return assistant.Scope{Kind: "note", NoteID: a.note.ID}, nil
	case 1:
		if a.selectedNotebook == "" || a.selectedNotebook == conflictsRail {
			return assistant.Scope{}, fmt.Errorf("choose a notebook first, or ask across all notes")
		}
		return assistant.Scope{Kind: "notebook", NotebookID: a.selectedNotebook, IncludeDescendants: a.ask.includeDesc}, nil
	default:
		return assistant.Scope{Kind: "vault"}, nil
	}
}

func (a *nativeApp) askSend() {
	if a.ask.busy || a.ask.historyBusy {
		return
	}
	mgr := a.askManager()
	if mgr == nil {
		a.ask.err = "Ask is unavailable in this build."
		return
	}
	if !mgr.Configured() {
		a.openAskSettings()
		a.ask.settingsErr = "Connect a model endpoint to start asking."
		return
	}
	question := strings.TrimSpace(a.ask.input)
	if question == "" {
		return
	}
	scope, err := a.askScope()
	if err != nil {
		a.ask.err = err.Error()
		return
	}
	// The scoped reader sees saved notes, so an unsaved draft is written first
	// through the normal editor path. The agent never receives a save tool.
	a.flush()
	handle, err := mgr.Start(assistant.AskRequest{ConversationID: a.ask.conversationID, Text: question, Scope: scope})
	if err != nil {
		a.ask.err = err.Error()
		return
	}
	a.ask.input = ""
	a.ask.err = ""
	a.ask.notice = ""
	a.ask.runID = handle.ID
	a.ask.busy = true
	a.ask.answer = ""
	a.ask.activity = "Starting"
	a.ask.citations = nil
	a.refreshAsk()
	// Start already recorded the user message; refreshAsk brought it back.
	if _, active := mgr.ActiveRun(); active {
		a.ask.runID = handle.ID
		a.ask.busy = true
	} else {
		// A storage failure can terminate the run before Start returns.
		a.ask.runID = ""
		a.ask.busy = false
		a.ask.activity = ""
	}
}

// resetAskConversation preserves the old scope's chat and starts a new thread;
// an old answer can never become evidence for a different note boundary.
func (a *nativeApp) resetAskConversation() {
	if len(a.ask.messages) == 0 {
		a.ask.notice = ""
		return
	}
	a.newAskChat("Scope changed. Started a new chat; the previous one is in History.")
}

func (a *nativeApp) currentAskTitle() string {
	title := ""
	for _, conversation := range a.ask.conversations {
		if conversation.ConversationID == a.ask.conversationID {
			title = conversation.Title
			break
		}
	}
	if strings.TrimSpace(title) == "" {
		for _, message := range a.ask.messages {
			if message.Role == "user" && strings.TrimSpace(message.Text) != "" {
				return clip(strings.Join(strings.Fields(message.Text), " "), 42)
			}
		}
		return "New chat"
	}
	return title
}

func (a *nativeApp) newAskChat(notice string) {
	if a.ask.historyBusy {
		return
	}
	mgr := a.askManager()
	if mgr == nil {
		return
	}
	if _, active := mgr.ActiveRun(); active {
		a.ask.err = "Stop the current Ask run before starting a new chat."
		return
	}
	a.ask.historyOpen = false
	a.ask.historyBusy = true
	a.ask.historyErr = ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		snapshot, err := mgr.NewConversation(ctx)
		mygo.RunOnMain(func() {
			a.ask.historyBusy = false
			if err != nil {
				a.ask.historyErr = err.Error()
			} else {
				a.ask.messages = snapshot.Messages
				a.ask.answer = ""
				a.ask.citations = nil
				a.ask.notice = notice
				a.refreshAsk()
			}
			if a.invalidate != nil {
				a.invalidate()
			}
		})
	}()
}

func (a *nativeApp) selectAskChat(id string) {
	mgr := a.askManager()
	if mgr == nil || id == a.ask.conversationID {
		a.ask.historyOpen = false
		return
	}
	if _, active := mgr.ActiveRun(); active {
		a.ask.historyErr = "Stop the current Ask run before switching chats."
		return
	}
	a.ask.historyOpen = false
	a.ask.historyBusy = true
	a.ask.historyErr = ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		snapshot, err := mgr.SelectConversation(ctx, id)
		mygo.RunOnMain(func() {
			a.ask.historyBusy = false
			if err != nil {
				a.ask.historyErr = err.Error()
			} else {
				a.ask.messages = snapshot.Messages
				a.ask.answer = ""
				a.ask.citations = nil
				a.ask.notice = "Loaded saved chat."
				a.refreshAsk()
			}
			if a.invalidate != nil {
				a.invalidate()
			}
		})
	}()
}

func (a *nativeApp) renameAskChat() {
	mgr := a.askManager()
	if mgr == nil || a.ask.renameID == "" {
		return
	}
	if err := mgr.RenameConversation(a.ask.renameID, a.ask.renameText); err != nil {
		a.ask.historyErr = err.Error()
		return
	}
	a.ask.renameID = ""
	a.ask.renameText = ""
	a.ask.historyErr = ""
	a.refreshAsk()
}

func (a *nativeApp) deleteAskChat() {
	mgr := a.askManager()
	if mgr == nil || a.ask.deleteID == "" {
		return
	}
	if _, active := mgr.ActiveRun(); active {
		a.ask.historyErr = "Stop the current Ask run before deleting a chat."
		return
	}
	id := a.ask.deleteID
	a.ask.historyBusy = true
	a.ask.deleteID = ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		snapshot, err := mgr.DeleteConversation(ctx, id)
		mygo.RunOnMain(func() {
			a.ask.historyBusy = false
			if err != nil {
				a.ask.historyErr = err.Error()
			} else {
				a.ask.messages = snapshot.Messages
				a.ask.answer = ""
				a.ask.citations = nil
				a.ask.notice = "Chat deleted."
				a.refreshAsk()
			}
			if a.invalidate != nil {
				a.invalidate()
			}
		})
	}()
}

func (a *nativeApp) askStop() {
	mgr := a.askManager()
	if mgr == nil || a.ask.runID == "" {
		return
	}
	if err := mgr.Cancel(a.ask.runID); err != nil {
		a.ask.err = err.Error()
	}
}

func (a *nativeApp) openAskSettings() {
	mgr := a.askManager()
	if mgr == nil {
		return
	}
	settings := mgr.Settings()
	a.ask.settings = settings
	a.ask.key = settings.APIKey
	a.ask.rememberKey = settings.RememberKey
	a.ask.contextText = strconv.Itoa(settings.ContextWindow)
	a.ask.maxText = strconv.Itoa(settings.MaxOutput)
	a.ask.settingsErr = ""
	a.ask.testNotice = ""
	a.ask.testing = false
	a.ask.settingsOpen = true
}

func (a *nativeApp) askSettingsDraft() (assistant.Settings, error) {
	settings := a.ask.settings
	settings.APIKey = strings.TrimSpace(a.ask.key)
	settings.RememberKey = a.ask.rememberKey
	contextWindow, err := strconv.Atoi(strings.TrimSpace(a.ask.contextText))
	if err != nil {
		return settings, fmt.Errorf("context window must be a number")
	}
	maxOutput, err := strconv.Atoi(strings.TrimSpace(a.ask.maxText))
	if err != nil {
		return settings, fmt.Errorf("maximum output must be a number")
	}
	settings.ContextWindow = contextWindow
	settings.MaxOutput = maxOutput
	settings.Normalize()
	if err := settings.Validate(); err != nil {
		return settings, err
	}
	return settings, nil
}

func (a *nativeApp) testAskSettings() {
	if a.ask.testing {
		return
	}
	mgr := a.askManager()
	if mgr == nil {
		return
	}
	settings, err := a.askSettingsDraft()
	if err != nil {
		a.ask.settingsErr = err.Error()
		return
	}
	a.ask.testing = true
	a.ask.settingsErr = ""
	a.ask.testNotice = ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		err := mgr.TestConnection(ctx, settings)
		mygo.RunOnMain(func() {
			a.ask.testing = false
			a.ask.connected = err == nil
			a.ask.connection = "Last test: connected to " + settings.ModelID + " · " + time.Now().Format("15:04")
			if err != nil {
				a.ask.settingsErr = err.Error()
				a.ask.connection = "Last test failed · " + clip(err.Error(), 110)
			} else {
				a.ask.testNotice = "Connection succeeded. Save to use this endpoint."
			}
			if a.invalidate != nil {
				a.invalidate()
			}
		})
	}()
}

func (a *nativeApp) saveAskSettings() {
	mgr := a.askManager()
	if mgr == nil {
		return
	}
	settings, err := a.askSettingsDraft()
	if err != nil {
		a.ask.settingsErr = err.Error()
		return
	}
	if err := mgr.ApplySettings(settings); err != nil {
		a.ask.settingsErr = err.Error()
		return
	}
	a.ask.settings = mgr.Settings()
	a.ask.settingsOpen = false
	a.ask.settingsErr = ""
	a.ask.testNotice = ""
	if settings.RememberKey {
		a.ask.notice = "Model connection saved. The API key is in your OS keychain."
	} else {
		a.ask.notice = "Model connection saved. The API key is kept for this session only."
	}
	a.ask.connection = "Connection saved; run Test connection to verify it."
	a.ask.connected = false
}

func (a *nativeApp) askSettingsDialog(c *ui.Context, t *ui.Theme) {
	if !a.ask.settingsOpen {
		return
	}
	ui.Modal(c, &a.ask.settingsOpen, func() {
		ui.Text(c, "Ask model connection").FontSize(sizeBody).Bold()
		ui.Text(c, "Questions and the note passages the agent reads are sent to this model endpoint, even when Joplin sync uses end-to-end encryption. The API key is never written to the note database.").
			FontSize(sizeSmall).TextColor(inkSoft).MaxWidth(460)
		ui.Scroll(c.Key("ask-settings-fields")).Width(480).MaxHeight(330).Children(func() {
			ui.TextInput(c.Key("ask-base-url"), &a.ask.settings.BaseURL).
				Label("Base URL (OpenAI-compatible, usually ends in /v1)").
				Placeholder("http://127.0.0.1:8080/v1").
				Width(460)
			ui.TextInput(c.Key("ask-model-id"), &a.ask.settings.ModelID).
				Label("Model ID").
				Placeholder("model-name").
				Width(460)
			ui.TextInput(c.Key("ask-api-key"), &a.ask.key).
				Label("API key (optional for local endpoints)").
				Placeholder("leave empty for a keyless local endpoint").
				Password().
				Width(460)
			ui.Checkbox(c, &a.ask.rememberKey, "Remember API key in the OS keychain")
			ui.Text(c, "Use this only on a trusted device. Leaving it off keeps the key in memory for this session only.").
				FontSize(sizeSmall).TextColor(inkSoft).MaxWidth(460)
			ui.Row(c).Gap(8).Children(func() {
				ui.TextInput(c.Key("ask-context"), &a.ask.contextText).
					Label("Context window (tokens)").Width(210)
				ui.TextInput(c.Key("ask-max-output"), &a.ask.maxText).
					Label("Max output (tokens)").Width(210)
			})
			ui.Text(c, "Only the configured scheme, host, port and API base path are allowed. Redirects are rejected; no web tools are installed.").
				FontSize(sizeSmall).TextColor(inkSoft).MaxWidth(460)
		})
		if a.ask.testing {
			ui.Text(c, "Testing the configured endpoint…").FontSize(sizeSmall).TextColor(inkSoft)
		}
		if a.ask.testNotice != "" {
			ui.Text(c, a.ask.testNotice).FontSize(sizeSmall).TextColor(inkNavy).MaxWidth(460)
		}
		if a.ask.settingsErr != "" {
			ui.Text(c, a.ask.settingsErr).FontSize(sizeSmall).TextColor(signalRed).MaxWidth(460)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "Cancel").Clicked() {
				a.ask.settingsOpen = false
			}
			if ui.Button(c, "Test connection").Clicked() {
				a.testAskSettings()
			}
			if primaryAction(c, "Save connection").Clicked() {
				a.saveAskSettings()
			}
		})
	})
}

// askPanel is the collapsible native Ask surface. It never renders HTML; the
// answer is text and verified citations are buttons that open their note.
func (a *nativeApp) askPanel(c *ui.Context, _ *ui.Theme) {
	panel := ui.Column(c).Shrink(0).Background(paperRaised).BorderStartWidth(1).BorderColor(hairline)
	windowWidth, _ := c.Size()
	innerWidth := a.ask.width - 36
	if a.narrow {
		panel.Grow(1).MinWidth(0).Clip()
		innerWidth = max(1, windowWidth-36)
	} else {
		if a.ask.width <= 0 {
			a.ask.width = askPanelWidth
		}
		innerWidth = a.ask.width - 36
		panel.Width(a.ask.width)
	}
	panel.Children(func() {
		var historyButton ui.Element
		ui.Row(c).Height(44).Shrink(0).Padding(0, 10, 0, 14).AlignItems(ui.Center).Gap(4).Children(func() {
			ui.Text(c, "Ask").Font(fontHeading).FontSize(18).TextColor(inkNavy)
			ui.Spacer(c)
			historyButton = ui.Button(c, "History").Tooltip("Open saved Ask chats")
			if historyButton.Clicked() {
				a.ask.historyOpen = !a.ask.historyOpen
			}
			if ui.Button(c, "New").Tooltip("Start a new Ask chat").Clicked() {
				a.ask.historyOpen = false
				a.newAskChat("Started a new chat.")
			}
			if ui.Button(c, "Model…").Clicked() {
				a.openAskSettings()
			}
			if iconButton(c, "ask-close", iconClose, "Close Ask", "Close the Ask panel").Clicked() {
				a.ask.open = false
			}
			a.askHistoryMenu(c, historyButton, innerWidth)
		})
		ui.Column(c).Shrink(0).Padding(0, 14, 6, 14).Gap(4).Children(func() {
			if ui.Segmented(c, &a.ask.scope, "This note", "Notebook", "All notes").Label("Search scope").Changed() {
				a.resetAskConversation()
			}
			if a.ask.scope == 1 {
				ui.Checkbox(c, &a.ask.includeDesc, "Include sub-notebooks")
			}
			ui.Text(c, "Only readable, non-conflict notes are included. Notes never change.").
				FontSize(sizeSmall).TextColor(inkSoft).MaxWidth(innerWidth)
			if a.ask.connection != "" {
				color := inkSoft
				if !a.ask.connected {
					color = amberInk
				}
				ui.Text(c, a.ask.connection).FontSize(sizeSmall).TextColor(color).MaxWidth(innerWidth)
			}
		})
		ui.Scroll(c.Key("ask-scroll")).Grow(1).MinHeight(0).Padding(0, 14, 10, 14).Children(func() {
			ui.Column(c).Gap(10).Width(innerWidth).Children(func() {
				for i, message := range a.ask.messages {
					role, color := "You", inkSoft
					if message.Role == "assistant" {
						role, color = "Answer", inkNavy
					} else if message.Role == "error" {
						role, color = "Could not answer", signalRed
					}
					ui.Text(c, role).FontSize(sizeSmall).Bold().TextColor(color)
					if message.Role == "assistant" {
						a.askAnswer(c, message.Text, message.Citations, strconv.Itoa(i), innerWidth)
						a.askCitationButtons(c, message.Citations, innerWidth)
					} else {
						ui.Text(c, message.Text).FontSize(sizeUI).LineHeight(1.5).TextColor(inkNavy).MaxWidth(innerWidth)
					}
				}
				if a.ask.answer != "" {
					ui.Text(c, "Answer").FontSize(sizeSmall).Bold().TextColor(inkNavy)
					a.askAnswer(c, a.ask.answer, a.ask.citations, "live", innerWidth)
					a.askCitationButtons(c, a.ask.citations, innerWidth)
				}
				if a.ask.activity != "" {
					ui.Text(c, a.ask.activity+"…").FontSize(sizeSmall).TextColor(amberInk)
				}
				if a.ask.notice != "" {
					ui.Text(c, a.ask.notice).FontSize(sizeSmall).TextColor(inkSoft).MaxWidth(innerWidth)
				}
				if a.ask.err != "" {
					ui.Text(c, a.ask.err).FontSize(sizeSmall).TextColor(signalRed).MaxWidth(innerWidth)
				}
			})
		})
		ui.Column(c).Shrink(0).Padding(0, 14, 12, 14).Gap(6).Children(func() {
			area := ui.TextArea(c.Key("ask-input"), &a.ask.input).
				Placeholder("Ask across your notes…").
				Label("Question").
				Font(fontSans).
				FontSize(sizeUI).
				LineHeight(1.45).
				Height(78).
				Width(innerWidth)
			if area.Submitted() {
				a.askSend()
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if a.ask.busy {
					if ui.Button(c, "Stop").Clicked() {
						a.askStop()
					}
				}
				mgr := a.askManager()
				if mgr == nil || !mgr.Configured() {
					if primaryAction(c, "Connect model").Clicked() {
						a.openAskSettings()
					}
					return
				}
				if primaryAction(c, "Send").Clicked() {
					a.askSend()
				}
			})
		})
	})
}

func (a *nativeApp) openAskCitation(citation assistant.Citation) {
	if citation.NoteID == "" {
		return
	}
	if mgr := a.askManager(); mgr != nil {
		current, err := mgr.CitationCurrent(citation)
		if err != nil {
			a.ask.err = err.Error()
			return
		}
		if !current {
			a.ask.notice = "Note changed since this answer. Saved excerpt: " + citation.Excerpt
			return
		}
	}
	if a.note != nil && a.note.ID == citation.NoteID {
		return
	}
	if note, err := a.svc.GetNote(citation.NoteID); err == nil && note != nil {
		if !a.noteInRows(citation.NoteID) {
			a.query = ""
			a.selectedNotebook = ""
			a.selectedTag = ""
			if note.IsConflict != 0 {
				a.selectedNotebook = conflictsRail
			}
			a.reloadNotes()
		}
		a.showNote(citation.NoteID)
		a.remember(citation.NoteID)
		return
	}
	a.ask.err = "That cited note is no longer available on this device."
}
