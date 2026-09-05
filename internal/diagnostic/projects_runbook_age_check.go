package diagnostic

import (
	"context"
	"fmt"
)

// CheckRunbookIndexAge is the doctor check for the freshness of the runbook
// index (RFC §10.5, step 1).
const CheckRunbookIndexAge = "runbook_index_age"

// runbookIndexMaxAgeDays is the age past which the index stops being
// trustworthy. The weekly LaunchAgent resyncs on Sundays, so two missed runs
// is the first symptom worth a warning.
const runbookIndexMaxAgeDays = 14

// RunbookIndexAgeCheck reports how long ago the runbook index was last
// rebuilt from the vault.
//
// The index is a derived cache of documents a human curates elsewhere: a
// stale one does not fail loudly, it quietly ranks a superseded runbook first
// or hides a new one, which is exactly the failure an operator cannot see
// from mem_runbook_find's output. `stale` on an individual row is a different
// signal — it says the document itself needs review — and both are reported
// so they are not confused with each other.
type RunbookIndexAgeCheck struct{}

func (RunbookIndexAgeCheck) Code() string { return CheckRunbookIndexAge }

func (c RunbookIndexAgeCheck) Run(ctx context.Context, scope Scope) (CheckResult, error) {
	_ = ctx
	freshness, err := scope.Store.RunbookIndexAge(scope.Project)
	if err != nil {
		return CheckResult{}, err
	}
	evidence := mustJSON(freshness)

	if !freshness.SchemaSetUp {
		return CheckResult{
			CheckID:      c.Code(),
			Result:       StatusOK,
			Severity:     SeverityInfo,
			ReasonCode:   c.Code() + "_schema_absent",
			Message:      "There is no runbook index in this database: the engram-projects schema is not present.",
			Why:          "Without the runbook_index table there is nothing to keep fresh, so an empty result is the correct state.",
			Evidence:     evidence,
			SafeNextStep: "No action required. Run `engram doctor --check projects_schema` if the schema was expected here.",
		}, nil
	}

	syncCommand := "engram projects runbooks sync --vault-dir <checkout>/vault/clarodrive"

	if freshness.Rows == 0 {
		return CheckResult{
			CheckID:      c.Code(),
			Result:       StatusOK,
			Severity:     SeverityInfo,
			ReasonCode:   c.Code() + "_empty",
			Message:      "The runbook index is empty.",
			Why:          "mem_runbook_find answers from this index alone, so until it is populated every symptom search comes back with nothing and the agent falls back to reading code.",
			Evidence:     evidence,
			SafeNextStep: "Populate it with `" + syncCommand + "`, or from an agent session with mem_runbook_index_sync.",
		}, nil
	}

	if freshness.OldestAgeD > runbookIndexMaxAgeDays {
		return CheckResult{
			CheckID:    c.Code(),
			Result:     StatusWarning,
			Severity:   SeverityWarning,
			ReasonCode: c.Code() + "_stale_index",
			Message: fmt.Sprintf("The oldest runbook index row was synced %d day(s) ago (threshold %d).",
				freshness.OldestAgeD, runbookIndexMaxAgeDays),
			Why:          "The index mirrors documents a human edits in the vault; once it lags, mem_runbook_find ranks on a picture of the vault that no longer exists — a renamed or retired runbook keeps being offered as a candidate.",
			Evidence:     evidence,
			SafeNextStep: "Run `" + syncCommand + " --prune-missing`, or check that the weekly com.clarodrive.engram-runbooks agent is still loaded.",
		}, nil
	}

	message := fmt.Sprintf("The runbook index holds %d row(s), oldest sync %d day(s) ago.",
		freshness.Rows, freshness.OldestAgeD)
	if freshness.StaleRows > 0 {
		message = fmt.Sprintf("%s %d of them are flagged stale by the vault itself.", message, freshness.StaleRows)
	}
	return CheckResult{
		CheckID:      c.Code(),
		Result:       StatusOK,
		Severity:     SeverityInfo,
		ReasonCode:   c.Code() + "_ok",
		Message:      message,
		Why:          "Every row was synced inside the freshness window, so the index reflects the vault as it stands.",
		Evidence:     evidence,
		SafeNextStep: "No action required.",
	}, nil
}
