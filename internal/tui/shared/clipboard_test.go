package shared

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// ─── OSC 52 sequence generation ──────────────────────────────────────────────

func TestOSC52SequenceForContent(t *testing.T) {
	content := "hello world"
	seq := OSC52Sequence(content)

	wantPrefix := "\x1b]52;c;"
	wantSuffix := "\x07"
	wantB64 := base64.StdEncoding.EncodeToString([]byte(content))

	if !strings.HasPrefix(seq, wantPrefix) {
		t.Fatalf("sequence does not start with OSC 52 prefix: %q", seq)
	}
	if !strings.HasSuffix(seq, wantSuffix) {
		t.Fatalf("sequence does not end with BEL: %q", seq)
	}
	middle := strings.TrimPrefix(strings.TrimSuffix(seq, wantSuffix), wantPrefix)
	if middle != wantB64 {
		t.Fatalf("base64 payload = %q, want %q", middle, wantB64)
	}
}

func TestOSC52SequenceEmptyContent(t *testing.T) {
	seq := OSC52Sequence("")
	wantB64 := base64.StdEncoding.EncodeToString([]byte(""))
	if !strings.Contains(seq, wantB64) {
		t.Fatalf("empty content sequence should still contain valid base64: %q", seq)
	}
}

func TestOSC52SequenceUnicodeContent(t *testing.T) {
	content := "Decisión de arquitectura 🐘"
	seq := OSC52Sequence(content)
	wantB64 := base64.StdEncoding.EncodeToString([]byte(content))
	if !strings.Contains(seq, wantB64) {
		t.Fatalf("unicode content not properly encoded: %q", seq)
	}
}

// ─── Copy command ─────────────────────────────────────────────────

func TestCopyReturnsCopiedMsg(t *testing.T) {
	cmd := Copy("test content")
	if cmd == nil {
		t.Fatal("Copy should return a non-nil command")
	}
	msg := cmd()
	_, ok := msg.(CopiedMsg)
	if !ok {
		t.Fatalf("command returned %T, want CopiedMsg", msg)
	}
}

func TestCopyMsgContainsSequence(t *testing.T) {
	content := "observation content"
	cmd := Copy(content)
	msg := cmd()
	cm, ok := msg.(CopiedMsg)
	if !ok {
		t.Fatalf("message type = %T", msg)
	}
	wantB64 := base64.StdEncoding.EncodeToString([]byte(content))
	if !strings.Contains(cm.Sequence, wantB64) {
		t.Fatalf("CopiedMsg.Sequence does not contain encoded content: %q", cm.Sequence)
	}
}

// ─── ClearFeedbackMsg timer ─────────────────────────────────────────────────

func TestClearFeedbackAfterReturnsCmd(t *testing.T) {
	cmd := ClearFeedbackAfter(1 * time.Millisecond)
	if cmd == nil {
		t.Fatal("ClearFeedbackAfter should return a non-nil command")
	}
	msg := cmd()
	_, ok := msg.(ClearFeedbackMsg)
	if !ok {
		t.Fatalf("message type = %T, want ClearFeedbackMsg", msg)
	}
}
