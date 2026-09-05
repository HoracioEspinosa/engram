package diagnostic

import (
	"context"
	"fmt"
	"strings"
)

// CheckProjectsSync is the doctor check for the replication backlog of the
// five engram-projects entities (RFC section 10.5, step 1).
const CheckProjectsSync = "projects_sync"

// ProjectsSyncCheck reports what engram-projects replication is carrying:
// mutations still waiting to be pushed, pulled rows parked because the row
// they reference has not arrived, and rows that will never apply on their own.
//
// It never repairs anything. The three outcomes map onto three different
// operator actions, which is why they are separated instead of collapsed into
// one "sync is behind" warning:
//
//   - dead rows from a jira_key collision need a human decision about which
//     ticket the task belongs to (`engram conflicts deferred --status dead`);
//   - deferred rows usually clear themselves on the next pull and otherwise
//     need `engram conflicts replay`;
//   - a pending backlog with replication switched off is not a fault at all,
//     it is the documented default of ADR-025.
type ProjectsSyncCheck struct{}

func (ProjectsSyncCheck) Code() string { return CheckProjectsSync }

func (c ProjectsSyncCheck) Run(ctx context.Context, scope Scope) (CheckResult, error) {
	_ = ctx
	schema, err := scope.Store.ProjectsSchemaStatus()
	if err != nil {
		return CheckResult{}, err
	}
	if !schema.Present {
		return CheckResult{
			CheckID:      c.Code(),
			Result:       StatusOK,
			Severity:     SeverityInfo,
			ReasonCode:   c.Code() + "_schema_absent",
			Message:      "engram-projects replication is inactive: the schema is not present in this database.",
			Why:          "With no project_cards/tasks/evidence tables there is nothing to replicate, so an empty backlog is the correct state.",
			Evidence:     mustJSON(map[string]any{"schema_present": false}),
			SafeNextStep: "No action required. Run `engram doctor --check projects_schema` if the schema was expected here.",
		}, nil
	}

	status, err := scope.Store.ProjectsSyncStatus(scope.Project)
	if err != nil {
		return CheckResult{}, err
	}
	evidence := mustJSON(status)

	if status.TotalDead > 0 {
		message := fmt.Sprintf("%d engram-projects mutation(s) will not apply without a manual decision.", status.TotalDead)
		nextStep := "Run `engram conflicts deferred --status dead` to see them."
		if status.KeyConflicts > 0 {
			message = fmt.Sprintf("%s %d of them lost a jira_key collision.", message, status.KeyConflicts)
			nextStep = "Run `engram conflicts deferred --status dead`, reconcile each task with " +
				"`engram project <slug> tasks upsert --jira <KEY> ...`, then `engram conflicts replay`."
		}
		return CheckResult{
			CheckID:              c.Code(),
			Result:               StatusWarning,
			Severity:             SeverityWarning,
			ReasonCode:           c.Code() + "_dead_mutations",
			Message:              message,
			Why:                  "A dead mutation is parked forever: its row exists on the replica that produced it and will never appear here until a human resolves the conflict.",
			Evidence:             evidence,
			SafeNextStep:         nextStep,
			RequiresConfirmation: true,
		}, nil
	}

	if status.TotalDeferred > 0 {
		return CheckResult{
			CheckID:      c.Code(),
			Result:       StatusWarning,
			Severity:     SeverityWarning,
			ReasonCode:   c.Code() + "_deferred_mutations",
			Message:      fmt.Sprintf("%d engram-projects mutation(s) are waiting for the row they reference.", status.TotalDeferred),
			Why:          "Evidence and task links are parked when their task or observation has not been pulled yet; they apply on their own once it arrives, but a backlog that never drains hides a missing parent row.",
			Evidence:     evidence,
			SafeNextStep: "Run `engram conflicts deferred` to see them and `engram conflicts replay` to retry now.",
		}, nil
	}

	if status.TotalPending > 0 && !status.Enabled {
		return CheckResult{
			CheckID:      c.Code(),
			Result:       StatusOK,
			Severity:     SeverityInfo,
			ReasonCode:   c.Code() + "_disabled",
			Message:      fmt.Sprintf("engram-projects replication is off; %d mutation(s) recorded before it was switched off are waiting.", status.TotalPending),
			Why:          "ENGRAM_PROJECTS_SYNC defaults to off until every binary on the team understands the new entities (ADR-025); nothing is lost while it is off.",
			Evidence:     evidence,
			SafeNextStep: "Set ENGRAM_PROJECTS_SYNC=1 once the cloud image and every teammate's binary are current.",
		}, nil
	}

	if status.TotalPending > 0 {
		return CheckResult{
			CheckID:      c.Code(),
			Result:       StatusOK,
			Severity:     SeverityInfo,
			ReasonCode:   c.Code() + "_pending",
			Message:      fmt.Sprintf("%d engram-projects mutation(s) are queued for the next push.", status.TotalPending),
			Why:          "A pending backlog is the normal state between two sync cycles; it only matters if it stops draining.",
			Evidence:     evidence,
			SafeNextStep: "Run `engram sync --cloud --project <slug>` or check `engram cloud status` if the queue is not draining.",
		}, nil
	}

	return CheckResult{
		CheckID:      c.Code(),
		Result:       StatusOK,
		Severity:     SeverityInfo,
		ReasonCode:   c.Code() + "_ok",
		Message:      projectsSyncOKMessage(status.Enabled),
		Why:          "No engram-projects mutation is queued, deferred or dead for the scope of this run.",
		Evidence:     evidence,
		SafeNextStep: "No action required.",
	}, nil
}

func projectsSyncOKMessage(enabled bool) string {
	state := "off"
	if enabled {
		state = "on"
	}
	return strings.Join([]string{
		"engram-projects replication has no backlog (ENGRAM_PROJECTS_SYNC is", state + ").",
	}, " ")
}
