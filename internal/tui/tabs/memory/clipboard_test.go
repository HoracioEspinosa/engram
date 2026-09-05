package memory

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/Gentleman-Programming/engram/internal/store"
	"github.com/Gentleman-Programming/engram/internal/tui/shared"
)

// ─── Update: shared.CopiedMsg sets CopyFeedback ────────────────────────────

func TestUpdateClipboardCopiedMsgSetsFeedback(t *testing.T) {
	m := New(nil, "")
	m.CopyFeedback = ""

	updatedModel, cmd := m.Update(shared.CopiedMsg{Sequence: "\x1b]52;c;aGVsbG8=\x07"})
	updated := updatedModel.(Model)

	if updated.CopyFeedback != "✓ Copied!" {
		t.Fatalf("CopyFeedback = %q, want %q", updated.CopyFeedback, "✓ Copied!")
	}
	if cmd == nil {
		t.Fatal("shared.CopiedMsg should return a clear-feedback command")
	}
}

// ─── Update: shared.ClearFeedbackMsg clears CopyFeedback ───────────────────────────

func TestUpdateClipboardClearMsgClearsFeedback(t *testing.T) {
	m := New(nil, "")
	m.CopyFeedback = "✓ Copied!"

	updatedModel, cmd := m.Update(shared.ClearFeedbackMsg{})
	updated := updatedModel.(Model)

	if updated.CopyFeedback != "" {
		t.Fatalf("CopyFeedback = %q, want empty string after clear", updated.CopyFeedback)
	}
	if cmd != nil {
		t.Fatal("shared.ClearFeedbackMsg should not return a command")
	}
}

// ─── 'c' key on ScreenRecent copies selected observation ─────────────────────

func TestRecentScreenCKeyCopiesToClipboard(t *testing.T) {
	m := New(nil, "")
	m.Screen = ScreenRecent
	m.RecentObservations = []store.Observation{
		{ID: 1, Content: "first observation content"},
		{ID: 2, Content: "second observation content"},
	}
	m.Cursor = 1

	updatedModel, cmd := m.handleRecentKeys("c")
	updated := updatedModel.(Model)
	_ = updated

	if cmd == nil {
		t.Fatal("'c' on recent screen should return a clipboard command")
	}
	msg := cmd()
	cm, ok := msg.(shared.CopiedMsg)
	if !ok {
		t.Fatalf("command returned %T, want shared.CopiedMsg", msg)
	}
	wantB64 := base64.StdEncoding.EncodeToString([]byte("second observation content"))
	if !strings.Contains(cm.Sequence, wantB64) {
		t.Fatalf("sequence does not contain expected content encoding")
	}
}

func TestRecentScreenCKeyWithNoObservationsIsNoop(t *testing.T) {
	m := New(nil, "")
	m.Screen = ScreenRecent
	m.RecentObservations = nil

	_, cmd := m.handleRecentKeys("c")
	if cmd != nil {
		t.Fatal("'c' with no observations should not return command")
	}
}

// ─── 'c' key on ScreenSearchResults copies selected result ───────────────────

func TestSearchResultsScreenCKeyCopiesToClipboard(t *testing.T) {
	m := New(nil, "")
	m.Screen = ScreenSearchResults
	m.SearchResults = []store.SearchResult{
		{Observation: store.Observation{ID: 1, Content: "search result one"}},
		{Observation: store.Observation{ID: 2, Content: "search result two"}},
	}
	m.Cursor = 0

	_, cmd := m.handleSearchResultsKeys("c")
	if cmd == nil {
		t.Fatal("'c' on search results screen should return a clipboard command")
	}
	msg := cmd()
	cm, ok := msg.(shared.CopiedMsg)
	if !ok {
		t.Fatalf("command returned %T, want shared.CopiedMsg", msg)
	}
	wantB64 := base64.StdEncoding.EncodeToString([]byte("search result one"))
	if !strings.Contains(cm.Sequence, wantB64) {
		t.Fatalf("sequence does not contain expected content encoding")
	}
}

func TestSearchResultsScreenCKeyWithNoResultsIsNoop(t *testing.T) {
	m := New(nil, "")
	m.Screen = ScreenSearchResults
	m.SearchResults = nil

	_, cmd := m.handleSearchResultsKeys("c")
	if cmd != nil {
		t.Fatal("'c' with no search results should not return command")
	}
}

// ─── 'c' key on ScreenObservationDetail copies full content ──────────────────

func TestObservationDetailScreenCKeyCopiesToClipboard(t *testing.T) {
	m := New(nil, "")
	m.Screen = ScreenObservationDetail
	m.SelectedObservation = &store.Observation{
		ID:      42,
		Content: "full observation content for copy",
	}

	_, cmd := m.handleObservationDetailKeys("c")
	if cmd == nil {
		t.Fatal("'c' on observation detail screen should return a clipboard command")
	}
	msg := cmd()
	cm, ok := msg.(shared.CopiedMsg)
	if !ok {
		t.Fatalf("command returned %T, want shared.CopiedMsg", msg)
	}
	wantB64 := base64.StdEncoding.EncodeToString([]byte("full observation content for copy"))
	if !strings.Contains(cm.Sequence, wantB64) {
		t.Fatalf("sequence does not contain expected content encoding")
	}
}

func TestObservationDetailScreenCKeyWithNoObservationIsNoop(t *testing.T) {
	m := New(nil, "")
	m.Screen = ScreenObservationDetail
	m.SelectedObservation = nil

	_, cmd := m.handleObservationDetailKeys("c")
	if cmd != nil {
		t.Fatal("'c' with nil observation should not return command")
	}
}

// ─── 'c' key on ScreenSessionDetail copies selected observation ───────────────

func TestSessionDetailScreenCKeyCopiesToClipboard(t *testing.T) {
	m := New(nil, "")
	m.Screen = ScreenSessionDetail
	m.SessionObservations = []store.Observation{
		{ID: 10, Content: "session obs one"},
		{ID: 11, Content: "session obs two"},
	}
	m.Cursor = 1

	_, cmd := m.handleSessionDetailKeys("c")
	if cmd == nil {
		t.Fatal("'c' on session detail screen should return a clipboard command")
	}
	msg := cmd()
	cm, ok := msg.(shared.CopiedMsg)
	if !ok {
		t.Fatalf("command returned %T, want shared.CopiedMsg", msg)
	}
	wantB64 := base64.StdEncoding.EncodeToString([]byte("session obs two"))
	if !strings.Contains(cm.Sequence, wantB64) {
		t.Fatalf("sequence does not contain expected content encoding")
	}
}

func TestSessionDetailScreenCKeyWithNoObservationsIsNoop(t *testing.T) {
	m := New(nil, "")
	m.Screen = ScreenSessionDetail
	m.SessionObservations = nil

	_, cmd := m.handleSessionDetailKeys("c")
	if cmd != nil {
		t.Fatal("'c' with no session observations should not return command")
	}
}

// ─── View: CopyFeedback appears in rendered output ───────────────────────────

func TestViewShowsCopyFeedback(t *testing.T) {
	m := New(nil, "")
	m.Width = 80
	m.Height = 24
	m.Screen = ScreenObservationDetail
	m.SelectedObservation = &store.Observation{
		ID:      1,
		Type:    "decision",
		Title:   "Test",
		Content: "content",
	}
	m.CopyFeedback = "✓ Copied!"

	view := m.View()
	if !strings.Contains(view, "✓ Copied!") {
		t.Fatal("view should display CopyFeedback when set")
	}
}

func TestViewDoesNotShowCopyFeedbackWhenEmpty(t *testing.T) {
	m := New(nil, "")
	m.Width = 80
	m.Height = 24
	m.Screen = ScreenObservationDetail
	m.SelectedObservation = &store.Observation{
		ID:      1,
		Type:    "decision",
		Title:   "Test",
		Content: "content",
	}
	m.CopyFeedback = ""

	view := m.View()
	if strings.Contains(view, "✓ Copied!") {
		t.Fatal("view should not show copy feedback when CopyFeedback is empty")
	}
}

// ─── View: help text includes 'c copy' on relevant screens ───────────────────

func TestViewRecentHelpTextIncludesCopy(t *testing.T) {
	m := New(nil, "")
	m.Width = 80
	m.Height = 24
	m.Screen = ScreenRecent
	m.RecentObservations = []store.Observation{{ID: 1, Type: "decision", Title: "t", Content: "c"}}

	view := m.viewRecent()
	if !strings.Contains(view, "c copy") {
		t.Fatalf("recent screen help text should include 'c copy', got: %q", view[strings.LastIndex(view, "\n")-100:])
	}
}

func TestViewSearchResultsHelpTextIncludesCopy(t *testing.T) {
	m := New(nil, "")
	m.Width = 80
	m.Height = 24
	m.Screen = ScreenSearchResults
	m.SearchResults = []store.SearchResult{{Observation: store.Observation{ID: 1}}}

	view := m.viewSearchResults()
	if !strings.Contains(view, "c copy") {
		t.Fatalf("search results help text should include 'c copy'")
	}
}

func TestViewObservationDetailHelpTextIncludesCopy(t *testing.T) {
	m := New(nil, "")
	m.Width = 80
	m.Height = 24
	m.Screen = ScreenObservationDetail
	m.SelectedObservation = &store.Observation{
		ID: 1, Type: "decision", Title: "t", Content: "content",
	}

	view := m.viewObservationDetail()
	if !strings.Contains(view, "c copy") {
		t.Fatalf("observation detail help text should include 'c copy'")
	}
}

func TestViewSessionDetailHelpTextIncludesCopy(t *testing.T) {
	m := New(nil, "")
	m.Width = 80
	m.Height = 24
	m.Screen = ScreenSessionDetail
	m.Sessions = []store.SessionSummary{{ID: "s1", Project: "engram"}}
	m.SelectedSessionIdx = 0
	m.SessionObservations = []store.Observation{{ID: 1, Type: "decision", Title: "t", Content: "c"}}

	view := m.viewSessionDetail()
	if !strings.Contains(view, "c copy") {
		t.Fatalf("session detail help text should include 'c copy'")
	}
}
