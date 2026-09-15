package tasks

import (
	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"

	tea "github.com/charmbracelet/bubbletea"
)

// loadTasks returns the command that lists tasksProject's tasks under f. The
// Tasks list and its search are folded into this one call — see
// data.TaskReader's doc comment for why there is no separate SearchTasks.
//
// It asks for the page, not the bare slice: the store counts the whole match
// in the same round trip, and without that count the footer can only report
// the page it is looking at and the page keys can only guess where the list
// ends.
func loadTasks(r data.TaskSource, tasksProject string, f store.TaskListFilter) tea.Cmd {
	return func() tea.Msg {
		page, err := r.ListTasksPage(tasksProject, f)
		return tasksLoadedMsg{page: page, err: err}
	}
}

// loadTaskDetail returns the command that loads one task's aggregate detail
// for the task detail screen: the row plus its linked observations and
// evidence.
func loadTaskDetail(r data.TaskSource, id int64) tea.Cmd {
	return func() tea.Msg {
		detail, err := r.Task(id)
		return taskDetailLoadedMsg{detail: detail, err: err}
	}
}

// updateTaskState returns the command that mirrors id's state into the local
// store: the TUI treats that store as the source of truth and never talks to
// Jira.
func updateTaskState(r data.TaskSource, id int64, state string) tea.Cmd {
	return func() tea.Msg {
		err := r.UpdateState(id, state)
		return stateUpdatedMsg{id: id, err: err}
	}
}

// linkObservation returns the command that links observationID to taskID,
// which is what "l" on the task detail screen asks for.
func linkObservation(r data.TaskSource, taskID, observationID int64) tea.Cmd {
	return func() tea.Msg {
		err := r.LinkObservation(taskID, observationID)
		return observationLinkedMsg{taskID: taskID, err: err}
	}
}

// loadContextPack returns the command that renders taskID's context pack,
// which "x" on the task detail screen asks for. It delegates to
// internal/project.BuildContextPack, the same function mem_context_pack calls.
func loadContextPack(r data.TaskSource, taskID int64) tea.Cmd {
	return func() tea.Msg {
		pack, err := r.ContextPack(taskID)
		return contextPackLoadedMsg{taskID: taskID, pack: pack, err: err}
	}
}
