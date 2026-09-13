package tasks

import (
	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"

	tea "github.com/charmbracelet/bubbletea"
)

// loadTasks returns the command that lists tasksProject's tasks under f
// (rfc-tui.md §9.2's "S3 Tasks list" / "S3 search" queries, folded into one
// call — see data.TaskReader's doc comment for why there is no separate
// SearchTasks).
func loadTasks(r data.TaskReader, tasksProject string, f store.TaskListFilter) tea.Cmd {
	return func() tea.Msg {
		items, err := r.ListTasks(tasksProject, f)
		return tasksLoadedMsg{items: items, err: err}
	}
}

// loadTaskDetail returns the command that loads one task's aggregate detail
// (S4): the row plus its linked observations and evidence.
func loadTaskDetail(r data.TaskReader, id int64) tea.Cmd {
	return func() tea.Msg {
		detail, err := r.Task(id)
		return taskDetailLoadedMsg{detail: detail, err: err}
	}
}

// updateTaskState returns the command that mirrors id's state locally
// (ADR-028: never talks to Jira).
func updateTaskState(r data.TaskReader, id int64, state string) tea.Cmd {
	return func() tea.Msg {
		err := r.UpdateState(id, state)
		return stateUpdatedMsg{id: id, err: err}
	}
}

// linkObservation returns the command that links observationID to taskID
// (S4, key "l").
func linkObservation(r data.TaskReader, taskID, observationID int64) tea.Cmd {
	return func() tea.Msg {
		err := r.LinkObservation(taskID, observationID)
		return observationLinkedMsg{taskID: taskID, err: err}
	}
}

// loadContextPack returns the command that renders taskID's context pack
// (S5, key "x" from S4; delegates to internal/project.BuildContextPack, the
// same function mem_context_pack calls).
func loadContextPack(r data.TaskReader, taskID int64) tea.Cmd {
	return func() tea.Msg {
		pack, err := r.ContextPack(taskID)
		return contextPackLoadedMsg{taskID: taskID, pack: pack, err: err}
	}
}
