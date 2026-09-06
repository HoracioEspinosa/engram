package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Gentleman-Programming/engram/internal/store"
)

// The CLI half of the engram -> vault promotion bridge (roadmap T-11.02).
//
//	engram project [<slug>] promote list  [--types decision,discovery] [--limit N] [--json]
//	engram project [<slug>] promote stamp <obs> --knowledge-ref <vault path> [--json]
//
// `list` is the read the knowledge repository's bridge consumes to render
// candidate documents; `stamp` is the write that records the merged document
// back on the observation. Neither touches the vault: the vault's only writer
// is a merged pull request.

func cmdProjectPromote(cfg store.Config, slug string, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: engram project [<slug>] promote <list|stamp> [flags]")
		exitFunc(1)
		return
	}
	switch args[0] {
	case "list":
		cmdProjectPromoteList(cfg, slug, args[1:])
	case "stamp":
		cmdProjectPromoteStamp(cfg, slug, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "engram: unknown promote subcommand %q\n", args[0])
		fmt.Fprintln(os.Stderr, "usage: engram project [<slug>] promote <list|stamp> [flags]")
		exitFunc(1)
	}
}

// projPromoteTypeFail maps a rejected type filter to the shared error code.
func projPromoteTypeFail(jsonOut bool, err error) bool {
	if !errors.Is(err, store.ErrTypeNotPromotable) {
		return false
	}
	projFail(jsonOut, "type_not_promotable", err.Error(),
		map[string]any{"hint": "the allowlist is " + strings.Join(store.PromotionAllowedTypes(), ", ") +
			"; widening it is a change to engram, not a flag"})
	return true
}

func cmdProjectPromoteList(cfg store.Config, slug string, args []string) {
	f := projNewFlags("engram project promote list")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	types := f.fs.String("types", "", "comma-separated subset of the allowlist (default: all of it)")
	limit := f.fs.Int("limit", 0, "cap the candidate list (0 = no cap)")
	if !f.parse(args) {
		return
	}
	if *limit < 0 {
		projFail(*jsonOut, "invalid_enum", "--limit must be zero or positive", nil)
		return
	}

	s, ok := projOpenStore(cfg)
	if !ok {
		return
	}
	defer s.Close()

	sc := projScopeForRead(s, slug, *jsonOut)
	if sc.Slug == "" {
		return
	}

	scan, err := s.PromotionCandidates(sc.Slug, projSplitList(*types), *limit)
	switch {
	case projPromoteTypeFail(*jsonOut, err):
		return
	case errors.Is(err, store.ErrProjectsSchemaMissing):
		// An empty result and an unrunnable scan are the same empty list, so
		// the second one has to be an error and not a quiet zero.
		projFail(*jsonOut, "projects_schema_missing", err.Error(),
			map[string]any{"hint": "run any engram command once against this database to migrate it"})
		return
	case err != nil:
		fatal(err)
		return
	}

	projPrintResult(*jsonOut, sc, scan, func() {
		fmt.Printf("pinned inspected: %d · candidates: %d · already promoted: %d · type-excluded: %d\n",
			scan.PinnedInspected, len(scan.Candidates), scan.AlreadyPromoted, scan.TypeExcluded)
		if scan.NotSyncable > 0 || scan.NoProject > 0 {
			fmt.Printf("not promotable as-is: %d without sync_id, %d without project\n",
				scan.NotSyncable, scan.NoProject)
		}
		fmt.Printf("allowlist: %s\n", strings.Join(scan.Types, ", "))
		if len(scan.Candidates) == 0 {
			fmt.Println("no candidates")
			return
		}
		t := &projTable{headers: []string{"ID", "SYNC_ID", "TYPE", "CREATED", "JIRA", "TITLE"}}
		for _, c := range scan.Candidates {
			t.add(
				fmt.Sprintf("%d", c.ObservationID),
				projShort(c.SyncID, 14),
				c.Type,
				projShort(c.CreatedAt, 10),
				projDash(c.JiraKey),
				projShort(c.Title, 56),
			)
		}
		t.render(os.Stdout)
	})
}

func cmdProjectPromoteStamp(cfg store.Config, slug string, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram project promote stamp")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	knowledgeRef := f.fs.String("knowledge-ref", "", "vault-relative path of the merged document")
	allowUnpinned := f.fs.Bool("allow-unpinned", false, "stamp an observation that is no longer pinned")
	allowAnyType := f.fs.Bool("allow-any-type", false, "stamp an observation outside the promotion allowlist")
	if !f.parse(rest) {
		return
	}

	if len(positional) == 0 {
		projFail(*jsonOut, "missing_field",
			"usage: engram project [<slug>] promote stamp <id|obs-hex> --knowledge-ref <path>", nil)
		return
	}
	if strings.TrimSpace(*knowledgeRef) == "" {
		projFail(*jsonOut, "missing_field", "--knowledge-ref is required", nil)
		return
	}

	s, ok := projOpenStore(cfg)
	if !ok {
		return
	}
	defer s.Close()

	sc := projScopeForRead(s, slug, *jsonOut)
	if sc.Slug == "" {
		return
	}

	obsID, ok := projResolveObservationID(s, positional[0], *jsonOut)
	if !ok {
		return
	}

	res, err := s.StampObservationKnowledgeRef(store.StampKnowledgeRefParams{
		ObservationID: obsID,
		KnowledgeRef:  *knowledgeRef,
		AllowUnpinned: *allowUnpinned,
		AllowAnyType:  *allowAnyType,
	})
	switch {
	case errors.Is(err, store.ErrUnknownObservation):
		projFail(*jsonOut, "unknown_observation", fmt.Sprintf("observation %q not found", positional[0]), nil)
		return
	case errors.Is(err, store.ErrObservationNotPinned):
		projFail(*jsonOut, "observation_not_pinned", err.Error(),
			map[string]any{"hint": "pin it first (mem_pin), or pass --allow-unpinned when repairing a merged document"})
		return
	case errors.Is(err, store.ErrObservationNotSyncable):
		projFail(*jsonOut, "observation_not_syncable", err.Error(), nil)
		return
	case errors.Is(err, store.ErrObservationNoProject):
		projFail(*jsonOut, "observation_no_project", err.Error(),
			map[string]any{"hint": "set the observation's project before stamping; the reference replicates per project"})
		return
	case errors.Is(err, store.ErrKnowledgeRefConflict):
		projFail(*jsonOut, "knowledge_ref_conflict", err.Error(),
			map[string]any{"hint": "one observation documents one curated fact; split the observation or fix the existing ref"})
		return
	case projPromoteTypeFail(*jsonOut, err):
		return
	case err != nil:
		if projKnowledgeRefFail(*jsonOut, err) {
			return
		}
		fatal(err)
		return
	}

	projPrintResult(*jsonOut, sc, res, func() {
		verb := "stamped"
		if !res.Stamped {
			verb = "already stamped"
		}
		fmt.Printf("%s observation %s (%s) -> %s\n", verb, res.ObservationSyncID, res.Type, res.KnowledgeRef)
	})
}
