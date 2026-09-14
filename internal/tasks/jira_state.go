// Package tasks derives engram-projects task lifecycle state from the
// literal Jira status strings mirrored by mem_task_upsert (RFC
// rfc-engram-projects.md §5.3).
package tasks

import (
	_ "embed"
	"encoding/json"
	"strings"
)

//go:embed jira_state_map.json
var jiraStateMapJSON []byte

// jiraStateMap maps a literal Jira status.name to the corresponding
// engram-projects task state. Loaded once from jira_state_map.json so the
// mapping table lives as data, not code, and can be extended without
// touching Go source.
var jiraStateMap = func() map[string]string {
	var m map[string]string
	if err := json.Unmarshal(jiraStateMapJSON, &m); err != nil {
		// The embedded file is part of the build; a decode failure here is a
		// programming error, not a runtime condition callers can recover from.
		panic("internal/tasks: invalid jira_state_map.json: " + err.Error())
	}
	return m
}()

// Valid task states, in the order the engram-projects schema CHECK
// constraint accepts them.
const (
	StateOpen       = "open"
	StateAnalysis   = "analysis"
	StateInProgress = "in_progress"
	StateReview     = "review"
	StateVerified   = "verified"
	StateDone       = "done"
	StateBlocked    = "blocked"
	StateCancelled  = "cancelled"
	// StatePending is work that reached its end and left something behind:
	// the vault calls it "Con pendientes", and the note saying what is left
	// lives on the task.
	StatePending = "pending"
	// StateArchived is work nobody is going to pick up again. It is closed,
	// like done and cancelled, but it never says the work was finished.
	StateArchived = "archived"
	// StateUnverified is work that looks finished and has not been confirmed
	// by anyone: the negation of verified, not a stage before it.
	StateUnverified = "unverified"
)

// AllStates lists every value the tasks.state CHECK constraint accepts, in the
// order the constraint spells them.
var AllStates = []string{
	StateOpen, StateAnalysis, StateInProgress, StateReview, StateVerified,
	StateDone, StateBlocked, StateCancelled, StatePending, StateArchived, StateUnverified,
}

// ValidState reports whether state is one of the values a task may hold.
func ValidState(state string) bool {
	for _, candidate := range AllStates {
		if candidate == state {
			return true
		}
	}
	return false
}

// ActiveStates lists every state considered "active" by mem_task_list's
// default filter: everything that is not closed. A task with pending work and
// one nobody has confirmed are both still somebody's problem.
var ActiveStates = []string{
	StateOpen, StateAnalysis, StateInProgress, StateReview, StateVerified,
	StateBlocked, StatePending, StateUnverified,
}

// ClosedStates lists the states that allow closed_at to be set — the states in
// which nothing further is expected to happen to the task.
var ClosedStates = map[string]bool{
	StateDone:      true,
	StateCancelled: true,
	StateArchived:  true,
}

// DeriveState resolves the task state for an incoming jira_status. It first
// looks up the literal status string in jira_state_map.json; when the status
// is unrecognized, it falls back to jiraStatusCategory (new -> open,
// indeterminate -> in_progress, done -> done). ok is false when neither the
// literal status nor the category could be mapped.
func DeriveState(jiraStatus, jiraStatusCategory string) (state string, ok bool) {
	if state, found := jiraStateMap[jiraStatus]; found {
		return state, true
	}
	switch strings.TrimSpace(jiraStatusCategory) {
	case "new":
		return StateOpen, true
	case "indeterminate":
		return StateInProgress, true
	case "done":
		return StateDone, true
	default:
		return "", false
	}
}
