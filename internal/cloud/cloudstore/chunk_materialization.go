package cloudstore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// ChunkMutationMaterializationReport says what one pass over cloud_chunks found
// and, when applied, what it wrote into cloud_mutations.
type ChunkMutationMaterializationReport struct {
	Project        string `json:"project"`
	Applied        bool   `json:"applied"`
	ChunksScanned  int    `json:"chunks_scanned"`
	Candidates     int    `json:"candidates"`
	AlreadyPresent int    `json:"already_present"`
	Invalid        int    `json:"invalid"`
	Materialized   int    `json:"materialized"`
}

// MaterializeChunkMutations walks the stored chunks of a project and writes into
// cloud_mutations every entity mutation that has no typed collection of its own
// and is not there yet.
//
// A push materializes those entities as it writes the chunk, but only for chunks
// written after that behavior existed. Everything a client pushed earlier still
// sits inside cloud_chunks alone — project cards, aliases, tasks, evidence,
// relations — and ListMutationsSince reads cloud_mutations, so a replica pulling
// the stream receives none of it while the dashboard, which counts chunks, shows
// the data as present. This pass closes that gap without touching chunk storage.
//
// It is idempotent: a mutation already present for the project under the same
// entity, key and op is left alone, so running it on every server start costs a
// scan and writes nothing once the backlog is drained.
func (cs *CloudStore) MaterializeChunkMutations(ctx context.Context, project string, apply bool) (ChunkMutationMaterializationReport, error) {
	if cs == nil || cs.db == nil {
		return ChunkMutationMaterializationReport{}, fmt.Errorf("cloudstore: not initialized")
	}
	project = strings.TrimSpace(project)
	if project == "" {
		return ChunkMutationMaterializationReport{}, fmt.Errorf("cloudstore: project is required")
	}

	report := ChunkMutationMaterializationReport{Project: project, Applied: apply}
	present, err := cs.existingMutationIdentities(ctx, project)
	if err != nil {
		return ChunkMutationMaterializationReport{}, err
	}

	rows, err := cs.db.QueryContext(ctx,
		`SELECT payload::text FROM cloud_chunks WHERE project_name = $1 ORDER BY chunk_id ASC`, project)
	if err != nil {
		return ChunkMutationMaterializationReport{}, fmt.Errorf("cloudstore: query chunks for materialization: %w", err)
	}
	defer rows.Close()

	missing := make([]MutationEntry, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return ChunkMutationMaterializationReport{}, fmt.Errorf("cloudstore: scan chunk for materialization: %w", err)
		}
		report.ChunksScanned++

		chunk, err := parseChunkData([]byte(payload))
		if err != nil {
			// An unparseable chunk is data the server already accepted; refusing
			// to start over it would be worse than skipping it here.
			report.Invalid++
			continue
		}
		for _, mutation := range chunk.Mutations {
			entity := strings.TrimSpace(mutation.Entity)
			if entity == "" || hasTypedChunkCollection(entity) {
				continue
			}
			entityKey := strings.TrimSpace(mutation.EntityKey)
			if entityKey == "" {
				report.Invalid++
				continue
			}
			op := strings.TrimSpace(mutation.Op)
			if op == "" {
				op = store.SyncOpUpsert
			}
			report.Candidates++

			identity := mutationIdentity(entity, entityKey, op)
			if _, ok := present[identity]; ok {
				report.AlreadyPresent++
				continue
			}
			present[identity] = struct{}{}

			body := json.RawMessage(strings.TrimSpace(mutation.Payload))
			if len(body) == 0 {
				body = json.RawMessage("{}")
			}
			missing = append(missing, MutationEntry{
				Project:   project,
				Entity:    entity,
				EntityKey: entityKey,
				Op:        op,
				Payload:   body,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return ChunkMutationMaterializationReport{}, fmt.Errorf("cloudstore: iterate chunks for materialization: %w", err)
	}

	if !apply || len(missing) == 0 {
		if !apply {
			report.Materialized = 0
		}
		return report, nil
	}

	tx, err := cs.db.BeginTx(ctx, nil)
	if err != nil {
		return ChunkMutationMaterializationReport{}, fmt.Errorf("cloudstore: begin chunk materialization tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	if err := insertMaterializedMutations(ctx, tx, missing); err != nil {
		return ChunkMutationMaterializationReport{}, err
	}
	if err := tx.Commit(); err != nil {
		return ChunkMutationMaterializationReport{}, fmt.Errorf("cloudstore: commit chunk materialization: %w", err)
	}
	tx = nil
	report.Materialized = len(missing)
	cs.invalidateDashboardReadModel()
	return report, nil
}

// MaterializeAllChunkMutations runs MaterializeChunkMutations over every project
// that has chunks, and is what the server calls on start.
func (cs *CloudStore) MaterializeAllChunkMutations(ctx context.Context, apply bool) ([]ChunkMutationMaterializationReport, error) {
	if cs == nil || cs.db == nil {
		return nil, fmt.Errorf("cloudstore: not initialized")
	}
	rows, err := cs.db.QueryContext(ctx, `
		SELECT DISTINCT project_name
		FROM cloud_chunks
		WHERE trim(coalesce(project_name, '')) <> ''
		ORDER BY project_name ASC`)
	if err != nil {
		return nil, fmt.Errorf("cloudstore: list chunk projects: %w", err)
	}
	projects := make([]string, 0)
	for rows.Next() {
		var project string
		if err := rows.Scan(&project); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("cloudstore: scan chunk project: %w", err)
		}
		projects = append(projects, project)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("cloudstore: iterate chunk projects: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("cloudstore: close chunk projects: %w", err)
	}

	reports := make([]ChunkMutationMaterializationReport, 0, len(projects))
	for _, project := range projects {
		report, err := cs.MaterializeChunkMutations(ctx, project, apply)
		if err != nil {
			return nil, err
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func (cs *CloudStore) existingMutationIdentities(ctx context.Context, project string) (map[string]struct{}, error) {
	rows, err := cs.db.QueryContext(ctx,
		`SELECT entity, entity_key, op FROM cloud_mutations WHERE project = $1`, project)
	if err != nil {
		return nil, fmt.Errorf("cloudstore: query materialized mutations: %w", err)
	}
	defer rows.Close()

	present := make(map[string]struct{})
	for rows.Next() {
		var entity, entityKey, op string
		if err := rows.Scan(&entity, &entityKey, &op); err != nil {
			return nil, fmt.Errorf("cloudstore: scan materialized mutation: %w", err)
		}
		present[mutationIdentity(entity, entityKey, op)] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cloudstore: iterate materialized mutations: %w", err)
	}
	return present, nil
}

// mutationIdentity keys a mutation by what makes it the same replicated fact.
// The payload is deliberately out: a later push of the same entity carries a
// newer payload and is a genuine update, not a row this pass should invent.
func mutationIdentity(entity, entityKey, op string) string {
	return strings.TrimSpace(entity) + "\x00" + strings.TrimSpace(entityKey) + "\x00" + strings.TrimSpace(op)
}
