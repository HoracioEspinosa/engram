// Engram — Persistent memory for AI coding agents.
//
// Usage:
//
//	engram serve          Start HTTP + MCP server
//	engram mcp            Start MCP server only (stdio transport)
//	engram search <query> Search memories from CLI
//	engram save           Save a memory from CLI
//	engram context        Show recent context
//	engram stats          Show memory stats
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/HoracioEspinosa/engram/internal/cloud/autosync"
	"github.com/HoracioEspinosa/engram/internal/cloud/constants"
	"github.com/HoracioEspinosa/engram/internal/cloud/remote"
	"github.com/HoracioEspinosa/engram/internal/cloud/syncguidance"
	"github.com/HoracioEspinosa/engram/internal/diagnostic"
	"github.com/HoracioEspinosa/engram/internal/mcp"
	"github.com/HoracioEspinosa/engram/internal/obsidian"
	"github.com/HoracioEspinosa/engram/internal/project"
	"github.com/HoracioEspinosa/engram/internal/server"
	"github.com/HoracioEspinosa/engram/internal/setup"
	"github.com/HoracioEspinosa/engram/internal/store"
	engramsync "github.com/HoracioEspinosa/engram/internal/sync"
	"github.com/HoracioEspinosa/engram/internal/timeutil"
	"github.com/HoracioEspinosa/engram/internal/tui"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"
	versioncheck "github.com/HoracioEspinosa/engram/internal/version"

	tea "github.com/charmbracelet/bubbletea"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// version is set via ldflags at build time by goreleaser.
// Falls back to "dev" for local builds; init() tries Go module info first.
var version = "dev"

func init() {
	if version != "dev" {
		return
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = strings.TrimPrefix(info.Main.Version, "v")
	}
}

var (
	storeNew      = store.New
	newHTTPServer = server.New
	startHTTP     = (*server.Server).Start

	newMCPServer           = mcp.NewServer
	newMCPServerWithTools  = mcp.NewServerWithTools
	newMCPServerWithConfig = mcp.NewServerWithConfig
	resolveMCPTools        = mcp.ResolveTools
	serveMCP               = mcpserver.ServeStdio

	// detectProject is injectable for testing; wraps project.DetectProject.
	detectProject = project.DetectProject

	newTUIModel = func(s *store.Store, project string, palette theme.Palette) tui.Model {
		return tui.New(s, version, project, palette)
	}
	newTeaProgram = tea.NewProgram
	runTeaProgram = (*tea.Program).Run

	checkForUpdates = versioncheck.CheckLatest

	setupSupportedAgents        = setup.SupportedAgents
	setupInstallAgent           = setup.Install
	setupAddClaudeCodeAllowlist = setup.AddClaudeCodeAllowlist
	scanInputLine               = fmt.Scanln

	storeSearch = func(s *store.Store, query string, opts store.SearchOptions) ([]store.SearchResult, error) {
		return s.Search(query, opts)
	}
	storeAddObservation    = func(s *store.Store, p store.AddObservationParams) (int64, error) { return s.AddObservation(p) }
	storeDeleteObservation = func(s *store.Store, id int64, hard bool) error { return s.DeleteObservation(id, hard) }
	storeDeleteSession     = func(s *store.Store, id string) error { return s.DeleteSession(id) }
	storeDeletePrompt      = func(s *store.Store, id int64) error { return s.DeletePrompt(id) }
	storeDeleteProject     = func(s *store.Store, name string, hard bool) (*store.DeleteProjectResult, error) {
		return s.DeleteProject(name, hard)
	}
	storeTimeline = func(s *store.Store, observationID int64, before, after int) (*store.TimelineResult, error) {
		return s.Timeline(observationID, before, after)
	}
	storeFormatContext = func(s *store.Store, project, scope string) (string, error) { return s.FormatContext(project, scope) }
	storeStats         = func(s *store.Store) (*store.Stats, error) { return s.Stats() }
	storeExport        = func(s *store.Store) (*store.ExportData, error) { return s.Export() }
	jsonMarshalIndent  = json.MarshalIndent
	runDiagnostics     = func(ctx context.Context, s *store.Store, project, check string) (diagnostic.Report, error) {
		runner := diagnostic.NewRunner()
		scope := diagnostic.Scope{Store: s, Project: project, Now: time.Now()}
		if strings.TrimSpace(check) != "" {
			return runner.RunOne(ctx, scope, check)
		}
		return runner.RunAll(ctx, scope)
	}

	syncStatus = func(sy *engramsync.Syncer) (localChunks int, remoteChunks int, pendingImport int, err error) {
		return sy.Status()
	}
	syncImport = func(sy *engramsync.Syncer) (*engramsync.ImportResult, error) { return sy.Import() }
	syncExport = func(sy *engramsync.Syncer, createdBy, project string) (*engramsync.SyncResult, error) {
		return sy.Export(createdBy, project)
	}
	newCloudAutosyncManager = func(s *store.Store, _ any) cloudAutosyncManager {
		mgr := autosync.New(s, nil, autosync.DefaultConfig())
		return autosyncManagerAdapter{manager: mgr}
	}

	// newAutosyncManager is the injectable factory used by tryStartAutosync.
	// BR2-3: Returns startableAutosyncManager (not *autosync.Manager) so tests can
	// inject a deterministic fake — preventing racy wg.Add/wg.Wait interleaving.
	newAutosyncManager = func(s *store.Store, transport autosync.CloudTransport, cfg autosync.Config) startableAutosyncManager {
		return autosync.New(s, transport, cfg)
	}

	exitFunc = os.Exit

	stdinScanner = func() *bufio.Scanner { return bufio.NewScanner(os.Stdin) }
	userHomeDir  = os.UserHomeDir

	// newObsidianExporter is injectable for testing.
	newObsidianExporter = obsidian.NewExporter

	// newObsidianWatcher is injectable for testing.
	newObsidianWatcher = obsidian.NewWatcher

	// agentRunnerFactory is injectable for testing. In production it delegates to
	// llm.NewRunner; tests substitute a fake to avoid real CLI invocations.
	agentRunnerFactory = defaultAgentRunnerFactory
)

type cloudSyncStatus struct {
	Phase               string
	LastError           string
	ConsecutiveFailures int
	BackoffUntil        *time.Time
	LastSyncAt          *time.Time
	ReasonCode          string
	ReasonMessage       string
}

type cloudAutosyncManager interface {
	Run(context.Context)
	NotifyDirty()
	Status() cloudSyncStatus
}

// startableAutosyncManager is the interface implemented by *autosync.Manager and used
// by tryStartAutosync. It combines autosyncStatusProvider with Run and Stop so that
// the factory variable newAutosyncManager can be stubbed in tests without spawning
// real goroutines — eliminating the racy wg.Add/wg.Wait interleaving.
// BR2-3: Using an interface return type (not *autosync.Manager) makes the factory
// injectable with deterministic fakes.
type startableAutosyncManager interface {
	autosyncStatusProvider // Status() autosync.Status
	Run(context.Context)
	Stop()
}

type autosyncManagerAdapter struct {
	manager *autosync.Manager
}

func (a autosyncManagerAdapter) Run(ctx context.Context) {
	a.manager.Run(ctx)
}

func (a autosyncManagerAdapter) NotifyDirty() {
	a.manager.NotifyDirty()
}

func (a autosyncManagerAdapter) Status() cloudSyncStatus {
	status := a.manager.Status()
	return cloudSyncStatus{
		Phase:               status.Phase,
		LastError:           status.LastError,
		ConsecutiveFailures: status.ConsecutiveFailures,
		BackoffUntil:        status.BackoffUntil,
		LastSyncAt:          status.LastSyncAt,
		ReasonCode:          status.ReasonCode,
		ReasonMessage:       status.ReasonMessage,
	}
}

// mutationTransportAdapter adapts remote.MutationTransport to autosync.CloudTransport.
// This bridges the type gap between packages without creating a circular import.
type mutationTransportAdapter struct {
	remote *remote.MutationTransport
}

func (a *mutationTransportAdapter) PushMutations(entries []autosync.MutationEntry) (*autosync.PushMutationsResult, error) {
	remoteEntries := make([]remote.MutationEntry, len(entries))
	for i, e := range entries {
		remoteEntries[i] = remote.MutationEntry{
			Project:   e.Project,
			Entity:    e.Entity,
			EntityKey: e.EntityKey,
			Op:        e.Op,
			Payload:   e.Payload,
		}
	}
	seqs, err := a.remote.PushMutations(remoteEntries)
	if err != nil {
		return nil, err
	}
	return &autosync.PushMutationsResult{AcceptedSeqs: seqs}, nil
}

func (a *mutationTransportAdapter) PullMutations(sinceSeq int64, limit int) (*autosync.PullMutationsResponse, error) {
	resp, err := a.remote.PullMutations(sinceSeq, limit)
	if err != nil {
		return nil, err
	}
	mutations := make([]autosync.PulledMutation, len(resp.Mutations))
	for i, m := range resp.Mutations {
		mutations[i] = autosync.PulledMutation{
			Seq:        m.Seq,
			Entity:     m.Entity,
			EntityKey:  m.EntityKey,
			Op:         m.Op,
			Payload:    m.Payload,
			OccurredAt: m.OccurredAt,
		}
	}
	return &autosync.PullMutationsResponse{
		Mutations: mutations,
		HasMore:   resp.HasMore,
		LatestSeq: resp.LatestSeq,
	}, nil
}

type storeSyncStatusProvider struct {
	store          *store.Store
	defaultProject string
	cfg            store.Config
}

func (p storeSyncStatusProvider) Status(project string) server.SyncStatus {
	resolvedProject, _ := store.NormalizeProject(project)
	resolvedProject = strings.TrimSpace(resolvedProject)
	if resolvedProject == "" {
		resolvedProject, _ = store.NormalizeProject(p.defaultProject)
		resolvedProject = strings.TrimSpace(resolvedProject)
	}
	upgradeStage, upgradeCode, upgradeMessage := p.upgradeStatus(resolvedProject)
	enabled, disabledCode, disabledMessage := p.cloudSyncEnabled(resolvedProject)
	targetKey := cloudTargetKeyForProject(resolvedProject)
	if !enabled {
		if disabledCode == "cloud_not_configured" && resolvedProject != "" {
			enrolled, err := p.store.IsProjectEnrolled(resolvedProject)
			if err != nil {
				return server.SyncStatus{
					Enabled:              false,
					Phase:                store.SyncLifecycleIdle,
					ReasonCode:           "status_unavailable",
					ReasonMessage:        fmt.Sprintf("cloud enrollment status is unavailable: %v", err),
					UpgradeStage:         upgradeStage,
					UpgradeReasonCode:    upgradeCode,
					UpgradeReasonMessage: upgradeMessage,
				}
			}
			if !enrolled {
				return server.SyncStatus{
					Enabled:              false,
					Phase:                store.SyncLifecycleIdle,
					ReasonCode:           constants.ReasonBlockedUnenrolled,
					ReasonMessage:        fmt.Sprintf("project %q is not enrolled for cloud sync", resolvedProject),
					UpgradeStage:         upgradeStage,
					UpgradeReasonCode:    upgradeCode,
					UpgradeReasonMessage: upgradeMessage,
				}
			}
			state, err := p.store.GetSyncState(targetKey)
			if err == nil && hasMeaningfulSyncState(state) {
				status := syncStatusFromState(state)
				status.Enabled = true
				status.UpgradeStage = upgradeStage
				status.UpgradeReasonCode = upgradeCode
				status.UpgradeReasonMessage = upgradeMessage
				return status
			}
		}
		return server.SyncStatus{
			Enabled:              false,
			Phase:                store.SyncLifecycleIdle,
			ReasonCode:           disabledCode,
			ReasonMessage:        disabledMessage,
			UpgradeStage:         upgradeStage,
			UpgradeReasonCode:    upgradeCode,
			UpgradeReasonMessage: upgradeMessage,
		}
	}
	state, err := p.store.GetSyncState(targetKey)
	if err != nil {
		reason := "sync state is unavailable"
		lastErr := fmt.Sprintf("read sync state: %v", err)
		return server.SyncStatus{
			Enabled:              true,
			Phase:                store.SyncLifecycleDegraded,
			ReasonCode:           "status_unavailable",
			ReasonMessage:        reason,
			LastError:            lastErr,
			UpgradeStage:         upgradeStage,
			UpgradeReasonCode:    upgradeCode,
			UpgradeReasonMessage: upgradeMessage,
		}
	}
	status := syncStatusFromState(state)
	status.Enabled = true
	status.UpgradeStage = upgradeStage
	status.UpgradeReasonCode = upgradeCode
	status.UpgradeReasonMessage = upgradeMessage
	return status
}

func (p storeSyncStatusProvider) upgradeStatus(project string) (string, string, string) {
	project = strings.TrimSpace(project)
	if project == "" {
		return "", "", ""
	}
	state, err := p.store.GetCloudUpgradeState(project)
	if err != nil {
		return "", "upgrade_status_unavailable", fmt.Sprintf("cloud upgrade status is unavailable: %v", err)
	}
	if state == nil {
		return "", "", ""
	}
	return state.Stage, strings.TrimSpace(state.LastErrorCode), strings.TrimSpace(state.LastErrorMessage)
}

func (p storeSyncStatusProvider) cloudSyncEnabled(project string) (bool, string, string) {
	cc, err := resolveCloudRuntimeConfig(p.cfg)
	if err != nil {
		return false, "cloud_config_error", fmt.Sprintf("cloud config error: %v", err)
	}
	if cc == nil || strings.TrimSpace(cc.ServerURL) == "" {
		return false, "cloud_not_configured", "cloud sync is not configured"
	}
	if _, err := validateCloudServerURL(cc.ServerURL); err != nil {
		return false, "cloud_config_error", fmt.Sprintf("cloud config error: invalid cloud runtime server URL: %v", err)
	}
	if strings.TrimSpace(project) == "" {
		return false, "project_required", "cloud sync status requires an explicit project scope"
	}
	enrolled, err := p.store.IsProjectEnrolled(project)
	if err != nil {
		return false, "status_unavailable", fmt.Sprintf("cloud enrollment status is unavailable: %v", err)
	}
	if !enrolled {
		return false, constants.ReasonBlockedUnenrolled, fmt.Sprintf("project %q is not enrolled for cloud sync", project)
	}
	return true, "", ""
}

func syncStatusFromState(state *store.SyncState) server.SyncStatus {
	var lastSyncAt *time.Time
	if state != nil && state.Lifecycle == store.SyncLifecycleHealthy {
		lastSyncAt = parseSyncStateTimestamp(state.UpdatedAt)
	}
	return server.SyncStatus{
		Phase:               state.Lifecycle,
		LastError:           derefString(state.LastError),
		ConsecutiveFailures: state.ConsecutiveFailures,
		BackoffUntil:        parseRFC3339Ptr(state.BackoffUntil),
		LastSyncAt:          lastSyncAt,
		ReasonCode:          derefString(state.ReasonCode),
		ReasonMessage:       derefString(state.ReasonMessage),
	}
}

func hasMeaningfulSyncState(state *store.SyncState) bool {
	if state == nil {
		return false
	}
	if state.Lifecycle != "" && state.Lifecycle != store.SyncLifecycleIdle {
		return true
	}
	if state.LastEnqueuedSeq > 0 || state.LastAckedSeq > 0 || state.LastPulledSeq > 0 {
		return true
	}
	if state.ConsecutiveFailures > 0 {
		return true
	}
	if state.BackoffUntil != nil || state.LeaseOwner != nil || state.LeaseUntil != nil {
		return true
	}
	if state.ReasonCode != nil || state.ReasonMessage != nil || state.LastError != nil {
		return true
	}
	return false
}

func parseSyncStateTimestamp(value string) *time.Time {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return &parsed
	}
	if parsed, err := time.ParseInLocation("2006-01-02 15:04:05", trimmed, time.UTC); err == nil {
		return &parsed
	}
	return nil
}

func parseRFC3339Ptr(value *string) *time.Time {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, *value)
	if err != nil {
		return nil
	}
	return &parsed
}

func derefString(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

func envBool(key string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func resolveCloudRuntimeConfig(cfg store.Config) (*cloudConfig, error) {
	cc, err := loadCloudConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("read cloud config: %w", err)
	}
	if cc == nil {
		cc = &cloudConfig{}
	}
	// ENGRAM_CLOUD_TOKEN overrides any token stored in cloud.json.
	// When the env var is absent, the persisted token from cloud.json is used
	// as a fallback so that `engram sync --cloud` works without requiring the
	// env var to be set in every shell session (fix for issue #343).
	if v := strings.TrimSpace(os.Getenv("ENGRAM_CLOUD_SERVER")); v != "" {
		cc.ServerURL = v
	}
	if v := strings.TrimSpace(os.Getenv("ENGRAM_CLOUD_TOKEN")); v != "" {
		cc.Token = v
	}
	return cc, nil
}

func preflightCloudSync(s *store.Store, cfg store.Config, project string, mutateState bool) (*cloudConfig, error) {
	project = strings.TrimSpace(project)
	if project != "" {
		project, _ = store.NormalizeProject(project)
	}
	targetKey := cloudTargetKeyForProject(project)

	cc, err := resolveCloudRuntimeConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("cloud sync config error: %w", err)
	}
	hasServer := strings.TrimSpace(cc.ServerURL) != ""
	if !hasServer {
		message := "cloud server is missing: configure server URL with `engram cloud config --server <url>`"
		if mutateState {
			_ = s.MarkSyncBlocked(targetKey, constants.ReasonCloudConfigError, message)
		}
		return nil, fmt.Errorf("cloud sync %s: %s", constants.ReasonCloudConfigError, message)
	}
	if _, err := validateCloudServerURL(cc.ServerURL); err != nil {
		message := fmt.Sprintf("invalid cloud runtime server URL: %v", err)
		if mutateState {
			_ = s.MarkSyncBlocked(targetKey, constants.ReasonCloudConfigError, message)
		}
		return nil, fmt.Errorf("cloud sync %s: %s", constants.ReasonCloudConfigError, message)
	}
	if project != "" {
		enrolled, err := s.IsProjectEnrolled(project)
		if err != nil {
			return nil, fmt.Errorf("cloud sync enrollment check: %w", err)
		}
		if !enrolled {
			message := fmt.Sprintf("project %q is not enrolled for cloud sync", project)
			if mutateState {
				_ = s.MarkSyncBlocked(targetKey, constants.ReasonBlockedUnenrolled, message)
			}
			return nil, fmt.Errorf("cloud sync blocked_unenrolled: %s", message)
		}
		if err := preflightCloudSyncLegacyMutations(s, project, targetKey, mutateState); err != nil {
			return nil, err
		}
	}
	return cc, nil
}

func preflightCloudSyncLegacyMutations(s *store.Store, project, targetKey string, mutateState bool) error {
	report, err := s.DiagnoseCloudUpgradeLegacyMutations(project)
	if err != nil {
		return fmt.Errorf("cloud sync legacy mutation preflight: %w", err)
	}
	if report.BlockedCount == 0 && report.RepairableCount == 0 {
		return nil
	}

	reasonCode := store.UpgradeReasonRepairableLegacyMutationPayload
	message := fmt.Sprintf(
		"legacy mutation payloads require repair before cloud sync for project %q: run `engram cloud upgrade doctor --project %s` then `engram cloud upgrade repair --project %s --apply`",
		project, project, project,
	)
	if report.BlockedCount > 0 {
		reasonCode = store.UpgradeReasonBlockedLegacyMutationManual
		first := firstBlockedLegacyMutationFinding(report)
		message = fmt.Sprintf(
			"legacy mutation payloads require manual action before cloud sync for project %q (seq=%d entity=%s op=%s): %s; inspect with `engram cloud upgrade doctor --project %s` and run `engram cloud upgrade repair --project %s --apply` for deterministic repairs",
			project, first.Seq, first.Entity, first.Op, first.Message, project, project,
		)
	}
	if mutateState {
		_ = s.MarkSyncBlocked(targetKey, reasonCode, message)
	}
	return fmt.Errorf("cloud sync %s: %s", reasonCode, message)
}

func firstBlockedLegacyMutationFinding(report store.CloudUpgradeLegacyMutationReport) store.CloudUpgradeLegacyMutationFinding {
	for _, finding := range report.Findings {
		if !finding.Repairable {
			return finding
		}
	}
	if len(report.Findings) > 0 {
		return report.Findings[0]
	}
	return store.CloudUpgradeLegacyMutationFinding{}
}

func cloudTargetKeyForProject(project string) string {
	project = strings.TrimSpace(project)
	if project == "" {
		return constants.TargetKeyCloud
	}
	project, _ = store.NormalizeProject(project)
	if strings.TrimSpace(project) == "" {
		return constants.TargetKeyCloud
	}
	return fmt.Sprintf("%s:%s", constants.TargetKeyCloud, project)
}

func markCloudSyncFailure(s *store.Store, targetKey string, syncErr error) {
	if syncErr == nil {
		return
	}
	message := cloudSyncFailureMessage(syncguidance.ProjectFromTargetKey(targetKey), syncErr)
	var statusErr *remote.HTTPStatusError
	if errors.As(syncErr, &statusErr) {
		switch {
		case statusErr.IsAuthFailure():
			_ = s.MarkSyncAuthRequired(targetKey, message)
			return
		case statusErr.IsPolicyFailure():
			_ = s.MarkSyncBlocked(targetKey, constants.ReasonPolicyForbidden, message)
			return
		}
	}
	_ = s.MarkSyncFailure(targetKey, message, time.Now().UTC().Add(30*time.Second))
}

func cloudSyncFailureMessage(project string, syncErr error) string {
	if syncErr == nil {
		return ""
	}
	return syncguidance.AppendGuidance(syncErr.Error(), project, syncErr)
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		exitFunc(1)
	}

	if shouldCheckForUpdates(os.Args[1:]) {
		printUpdateCheckResult(checkForUpdates(version))
	}
	if handleConfigFreeCommand(os.Args[1:]) {
		return
	}

	cfg, cfgErr := store.DefaultConfig()
	if cfgErr != nil {
		// Fallback: try to resolve home directory from environment variables
		// that os.UserHomeDir() might have missed (e.g. MCP subprocesses on
		// Windows where %USERPROFILE% is not propagated).
		if home := resolveHomeFallback(); home != "" {
			log.Printf("[engram] UserHomeDir failed, using fallback: %s", home)
			cfg = store.FallbackConfig(filepath.Join(home, ".engram"))
		} else {
			fatal(cfgErr)
		}
	}

	// Allow overriding data dir via env
	if dir := os.Getenv("ENGRAM_DATA_DIR"); dir != "" {
		cfg.DataDir = dir
	}

	// Migrate orphaned databases that ended up in wrong locations
	// (e.g. drive root on Windows due to previous bug).
	migrateOrphanedDB(cfg.DataDir)

	switch os.Args[1] {
	case "serve":
		cmdServe(cfg)
	case "mcp":
		cmdMCP(cfg)
	case "tui":
		cmdTUI(cfg)
	case "search":
		cmdSearch(cfg)
	case "save":
		cmdSave(cfg)
	case "delete":
		cmdDelete(cfg)
	case "timeline":
		cmdTimeline(cfg)
	case "conflicts":
		cmdConflicts(cfg)
	case "doctor":
		cmdDoctor(cfg)
	case "context":
		cmdContext(cfg)
	case "stats":
		cmdStats(cfg)
	case "export":
		cmdExport(cfg)
	case "import":
		cmdImport(cfg)
	case "sync":
		cmdSync(cfg)
	case "cloud":
		cmdCloud(cfg)
	case "obsidian-export":
		cmdObsidianExport(cfg)
	case "projects":
		cmdProjects(cfg)
	case "project":
		cmdProject(cfg)
	case "theme":
		cmdTheme(cfg)
	case "setup":
		cmdSetup(cfg)
	case "protocol-mode":
		cmdProtocolMode(cfg)
	case "version", "--version", "-v":
		fmt.Printf("engram %s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		printUsage()
		exitFunc(1)
	}
}

func shouldCheckForUpdates(args []string) bool {
	if len(args) == 0 {
		return false
	}
	command := strings.ToLower(strings.TrimSpace(args[0]))
	switch command {
	case "mcp", "serve", "protocol-mode":
		return false
	case "cloud":
		return len(args) < 2 || strings.ToLower(strings.TrimSpace(args[1])) != "serve"
	}
	return true
}

func handleConfigFreeCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "version", "--version", "-v":
		fmt.Printf("engram %s\n", version)
		return true
	case "help", "--help", "-h":
		printUsage()
		return true
	case "cloud":
		if len(args) >= 2 {
			subcommand := strings.ToLower(strings.TrimSpace(args[1]))
			if subcommand == "--help" || subcommand == "-h" || subcommand == "help" {
				cmdCloud(store.Config{})
				return true
			}
		}
	}
	return false
}

func printUpdateCheckResult(result versioncheck.CheckResult) {
	if result.Status != versioncheck.StatusUpToDate && result.Message != "" {
		fmt.Fprintln(os.Stderr, result.Message)
		fmt.Fprintln(os.Stderr)
	}
}

// ─── Commands ────────────────────────────────────────────────────────────────

func cmdServe(cfg store.Config) {
	port := 7437 // "ENGR" on phone keypad vibes
	if p := os.Getenv("ENGRAM_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}
	// Allow: engram serve 8080
	if len(os.Args) > 2 {
		if n, err := strconv.Atoi(os.Args[2]); err == nil {
			port = n
		}
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	srv := newHTTPServer(s, port)

	// Wire the semantic runner factory and prompt builder for POST /conflicts/scan.
	// Both live in cmd/engram so internal/server avoids a direct dependency on internal/llm.
	srv.SetRunnerFactory(agentRunnerFactory)
	srv.SetPromptBuilder(func(a, b store.ObservationSnippet) string {
		return llmBuildPrompt(a, b)
	})

	// Graceful shutdown context — cancelled on SIGINT/SIGTERM.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Try to start autosync (opt-in via ENGRAM_CLOUD_AUTOSYNC=1).
	// BW7: tryStartAutosync returns (status provider, stop func) so the signal
	// handler can call mgrStop() before os.Exit, giving the manager time to
	// release its sync lease.
	fallback := storeSyncStatusProvider{store: s, defaultProject: resolveServeSyncStatusProject(), cfg: cfg}
	mgr, mgrStop := tryStartAutosync(ctx, s, cfg)
	if mgr != nil {
		srv.SetSyncStatus(&autosyncStatusAdapter{mgr: mgr, fallback: fallback})
	} else {
		srv.SetSyncStatus(fallback)
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("[engram] shutting down...")
		cancel()
		if mgrStop != nil {
			mgrStop() // BW7: wait for Manager to release lease before exiting
		}
		exitFunc(0)
	}()

	if err := startHTTP(srv); err != nil {
		fatal(err)
	}
}

func resolveServeSyncStatusProject() string {
	projectName := strings.TrimSpace(os.Getenv("ENGRAM_PROJECT"))
	if projectName == "" {
		if cwd, err := os.Getwd(); err == nil {
			projectName = detectProject(cwd)
		}
	}
	projectName, _ = store.NormalizeProject(projectName)
	return strings.TrimSpace(projectName)
}

// tryStartAutosync starts the autosync Manager if ENGRAM_CLOUD_AUTOSYNC=1 and
// both ENGRAM_CLOUD_TOKEN and ENGRAM_CLOUD_SERVER are present. Opt-in requires
// exactly "1"; a missing token or server logs and skips. Never fatal —
// autosync is optional.
// BW7: Returns (status provider, stop func) so the caller can invoke stop
// before os.Exit to ensure the Manager releases its sync lease.
func tryStartAutosync(ctx context.Context, s *store.Store, cfg store.Config) (autosyncStatusProvider, func()) {
	// Opt-in requires exactly "1".
	if strings.TrimSpace(os.Getenv("ENGRAM_CLOUD_AUTOSYNC")) != "1" {
		return nil, nil
	}

	cc, err := resolveCloudRuntimeConfig(cfg)
	if err != nil {
		log.Printf("[autosync] ERROR: cannot read cloud config: %v", err)
		return nil, nil
	}

	token := strings.TrimSpace(cc.Token)
	serverURL := strings.TrimSpace(cc.ServerURL)

	// A token is required. It is resolved from cloud.json first and
	// overridden by ENGRAM_CLOUD_TOKEN when set, so both sources are tried.
	// On Windows (Task Scheduler), the env var is often absent — the file path
	// is the expected source (issue #421).
	if token == "" {
		log.Printf("[autosync] ERROR: cloud token is not configured (set ENGRAM_CLOUD_TOKEN or store token in cloud.json via `engram cloud config`); autosync disabled")
		return nil, nil
	}
	// A server URL is required too. Resolved from cloud.json or ENGRAM_CLOUD_SERVER.
	if serverURL == "" {
		log.Printf("[autosync] ERROR: cloud server URL is not configured (set ENGRAM_CLOUD_SERVER or run `engram cloud config --server <url>`); autosync disabled")
		return nil, nil
	}

	remoteMT, err := remote.NewMutationTransport(serverURL, token)
	if err != nil {
		log.Printf("[autosync] ERROR: invalid server URL %q: %v; autosync disabled", serverURL, err)
		return nil, nil
	}
	transport := &mutationTransportAdapter{remote: remoteMT}
	mgrCfg := autosync.DefaultConfig()
	// BR2-3: Call newAutosyncManager (injectable) instead of autosync.New directly,
	// so tests can stub the factory and avoid real goroutine/network side effects.
	mgr := newAutosyncManager(s, transport, mgrCfg)

	go mgr.Run(ctx)
	log.Printf("[autosync] started (server=%s)", serverURL)
	return mgr, mgr.Stop
}

func cmdMCP(cfg store.Config) {
	toolsFilter := ""
	projectOverride := strings.TrimSpace(os.Getenv("ENGRAM_PROJECT"))
	for i := 2; i < len(os.Args); i++ {
		if strings.HasPrefix(os.Args[i], "--tools=") {
			toolsFilter = strings.TrimPrefix(os.Args[i], "--tools=")
		} else if os.Args[i] == "--tools" && i+1 < len(os.Args) {
			toolsFilter = os.Args[i+1]
			i++
		} else if strings.HasPrefix(os.Args[i], "--project=") {
			projectOverride = strings.TrimSpace(strings.TrimPrefix(os.Args[i], "--project="))
			if projectOverride == "" {
				fatal(fmt.Errorf("--project requires a value"))
			}
		} else if os.Args[i] == "--project" {
			if i+1 >= len(os.Args) {
				fatal(fmt.Errorf("--project requires a value"))
			}
			projectOverride = strings.TrimSpace(os.Args[i+1])
			if projectOverride == "" {
				fatal(fmt.Errorf("--project requires a value"))
			}
			i++
		}
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	// Match `engram serve` autosync startup semantics for stdio MCP agents.
	// Autosync remains opt-in via ENGRAM_CLOUD_AUTOSYNC=1 and never makes MCP
	// startup fatal when cloud config is missing or invalid.
	ctx, cancel := context.WithCancel(context.Background())
	_, mgrStop := tryStartAutosync(ctx, s, cfg)
	autosyncStopped := false
	stopAutosync := func() {
		if autosyncStopped {
			return
		}
		autosyncStopped = true
		cancel()
		if mgrStop != nil {
			mgrStop()
		}
	}
	defer stopAutosync()

	mcpCfg := mcp.MCPConfig{DefaultProject: projectOverride, ServerVersion: version}
	allowlist := resolveMCPTools(toolsFilter)
	mcpSrv := newMCPServerWithConfig(s, mcpCfg, allowlist)

	if err := serveMCP(mcpSrv); err != nil {
		stopAutosync()
		fatal(err)
	}
}

func cmdTUI(cfg store.Config) {
	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	// The binary's own palettes are seeded on every start, not at install
	// time, so a palette a later version ships appears without anybody
	// re-running a setup step. Seeding leaves an edited row alone, and a
	// failure here is not worth refusing to open the workspace over: the
	// compiled registry answers on its own.
	if err := themeSeed(s); err != nil {
		fmt.Fprintf(os.Stderr, "engram: could not seed the built-in themes: %v\n", err)
	}

	selection := resolveTUITheme(s, cfg)
	if unknown := selection.UnknownName(); unknown != "" {
		fmt.Fprintf(os.Stderr, "engram: unknown theme %q, falling back to %s\n", unknown, theme.DefaultThemeName)
	}
	palette := selection.Resolve()
	project := resolveTUIProject(s)
	model := newTUIModel(s, project, palette).
		WithIcons(resolveTUIIcons(s)).
		WithLastTab(resolveTUITab(s, project))
	p := newTeaProgram(model, tuiProgramOptions(resolveTUIMouse(s))...)
	if _, err := runTeaProgram(p); err != nil {
		fatal(err)
	}
}

// Settings keys `engram tui` reads to reopen where it was left. They are
// spelled here and again in internal/tui/app, the way tui.theme already is:
// internal/tui is reached through one facade, and a settings key is not part
// of that facade's surface.
const (
	lastProjectSettingKey = "tui.last_project"
	lastTabSettingKey     = "tui.last_tab"
	iconsSettingKey       = "tui.icons"
)

// tuiSetting reads one remembered value, or "" when there is none and when
// the store will not answer. Nothing here is worth refusing to open the
// workspace over: every caller has a default that is exactly the state a fresh
// installation is in.
func tuiSetting(s *store.Store, key string) string {
	if s == nil {
		return ""
	}
	value, ok, err := s.Setting(key)
	if err != nil {
		log.Printf("[engram] ignoring the remembered %s: %v", key, err)
		return ""
	}
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

// resolveTUIIcons picks the glyph vocabulary the workspace draws with:
// ENGRAM_TUI_ICONS, then settings['tui.icons'], then whatever TERM and the
// locale say the terminal can be trusted to render. Nerd Font icons are never
// inferred — see theme.ResolveIconMode.
func resolveTUIIcons(s *store.Store) theme.IconMode {
	return theme.ResolveIconMode(
		os.Getenv("ENGRAM_TUI_ICONS"),
		tuiSetting(s, iconsSettingKey),
		os.Getenv("TERM"),
		os.Getenv("LANG"),
	)
}

// resolveTUITab names the tab the workspace reopens on, and "" for the tab
// New would have chosen on its own.
//
// The remembered tab only applies to the project it was remembered against.
// Opening a different project on the last one's tab shows a screen about
// somebody else's work, which is worse than the predictable landing screen: a
// tab is a place inside a project, not a global preference.
func resolveTUITab(s *store.Store, project string) string {
	if project == "" {
		return ""
	}
	if tuiSetting(s, lastProjectSettingKey) != project {
		return ""
	}
	return tuiSetting(s, lastTabSettingKey)
}

// mouseSettingKey is where the pointer is turned off for good. It is the same
// key theme_cmd.go's themeSettingKey pattern establishes: the literal is
// spelled in cmd/engram because internal/tui is reached through one facade
// and a settings key is not part of that facade's surface.
const mouseSettingKey = "tui.mouse"

// tuiProgramOptions are the Bubble Tea options `engram tui` opens with.
//
// The alternate screen is an option rather than a command the model returns
// from Init, and that is the whole difference: the program writes its first
// frame before it processes its first message, so a switch asked for as a
// command arrives one frame late and leaves that frame printed in the shell
// the user comes back to.
//
// Cell motion is what makes a click land on a cell rather than on a pixel the
// program cannot reason about, and it is the mode the tab bar's hitboxes and
// the wheel both assume. It is a separate function from cmdTUI so the decision
// can be tested: a tea.ProgramOption is an opaque closure, so what a test can
// check is that the option is there at all, or that it is not.
func tuiProgramOptions(mouse bool) []tea.ProgramOption {
	options := []tea.ProgramOption{tea.WithAltScreen()}
	if mouse {
		options = append(options, tea.WithMouseCellMotion())
	}
	return options
}

// resolveTUIMouse answers whether the workspace opens with the mouse enabled:
// --no-mouse on the command line first, then settings['tui.mouse'] = "off".
//
// Enabling the mouse takes the terminal's own selection away — every drag
// becomes an event the program consumes — so both a one-off escape hatch and a
// remembered one are needed. Everything but an explicit "off" leaves the mouse
// on: a settings row nobody wrote, or a value nobody recognises, must not
// quietly disable a feature.
func resolveTUIMouse(s *store.Store) bool {
	for i := 2; i < len(os.Args); i++ {
		if os.Args[i] == "--no-mouse" {
			return false
		}
	}
	if s == nil {
		return true
	}
	value, ok, err := s.Setting(mouseSettingKey)
	if err != nil {
		log.Printf("[engram] ignoring the remembered mouse setting: %v", err)
		return true
	}
	if !ok {
		return true
	}
	return strings.ToLower(strings.TrimSpace(value)) != "off"
}

// resolveTUIProject resolves the project `engram tui` opens on, following
// a fixed precedence for --project: an explicit --project
// (or --project=) flag first, then ENGRAM_PROJECT, then cwd detection, then
// the project the last run was left on. An empty result means no project was
// resoluble, so the workspace opens on its no-project home instead of a
// Dashboard.
//
// cwd detection only counts as resoluble when project.DetectProjectFull backs
// it with a fact (a git-derived source or repo config) — a directory-name
// guess (project.SourceDirBasename) is deliberately treated the same as no
// detection at all: without this, DetectProject's bare string never came
// back empty, so a non-git directory always looked resoluble and the
// workspace's project tree fallback could never be reached.
//
// The remembered project sits below that detection and never above it. Opening
// a terminal inside a repository and being shown a different project's
// workspace is the one failure a remembered choice must not cause: where the
// user is standing is a fact, and a fact outranks a habit. It sits above the
// empty selector, which is what the tier is for — a workspace opened from a
// directory that is nobody's project reopens on the project it was last doing
// work in, rather than on a list to pick from again.
func resolveTUIProject(s *store.Store) string {
	resolved := ""
	for i := 2; i < len(os.Args); i++ {
		switch {
		case os.Args[i] == "--project" && i+1 < len(os.Args):
			resolved = os.Args[i+1]
			i++
		case strings.HasPrefix(os.Args[i], "--project="):
			resolved = strings.TrimPrefix(os.Args[i], "--project=")
		}
	}
	if resolved == "" {
		resolved = strings.TrimSpace(os.Getenv("ENGRAM_PROJECT"))
	}
	if resolved == "" {
		if cwd, err := os.Getwd(); err == nil {
			if det := detectProjectFull(cwd); det.Error == nil && !project.IsGuessedSource(det.Source) {
				resolved = det.Project
			}
		}
	}
	if resolved == "" {
		resolved = tuiSetting(s, lastProjectSettingKey)
	}
	if resolved == "" {
		return ""
	}
	normalized, warning := store.NormalizeProject(resolved)
	if warning != "" {
		fmt.Fprintln(os.Stderr, warning)
	}
	return normalized
}

// isGuessedProjectSource reports whether a detectProjectFull source is a
// directory-name guess rather than a fact the detector actually knows.
// It exists as a plain function, not a direct
// project.IsGuessedSource call, because several call sites below name a
// local "project" variable that would otherwise shadow the project package
// import.
func isGuessedProjectSource(source string) bool {
	return project.IsGuessedSource(source)
}

// resolveTUITheme gathers everything that has an opinion about which palette
// the TUI opens on: an explicit --theme (or --theme=) flag, ENGRAM_TUI_THEME,
// the tui.theme setting, the tui.theme key in <data-dir>/config.json, and the
// palettes the themes table holds.
//
// Picking the winner and falling back to the default is theme.Selection's job,
// not this function's — resolveTUIProject inlines that choice because it has
// no registry to validate against, but Selection is exactly that
// registry-aware chooser, so this stays a plain gatherer.
//
// Neither the setting nor the stored palettes are worth failing over. A store
// that will not answer means the compiled registry answers alone, which is the
// same state a fresh installation is in.
func resolveTUITheme(s *store.Store, cfg store.Config) theme.Selection {
	selection := theme.Selection{
		Env:    strings.TrimSpace(os.Getenv("ENGRAM_TUI_THEME")),
		Config: readTUIThemeConfig(cfg),
	}
	for i := 2; i < len(os.Args); i++ {
		switch {
		case os.Args[i] == "--theme" && i+1 < len(os.Args):
			selection.Flag = os.Args[i+1]
			i++
		case strings.HasPrefix(os.Args[i], "--theme="):
			selection.Flag = strings.TrimPrefix(os.Args[i], "--theme=")
		}
	}
	if s == nil {
		return selection
	}

	if value, ok, err := s.Setting(themeSettingKey); err != nil {
		log.Printf("[engram] ignoring the remembered theme: %v", err)
	} else if ok {
		selection.Setting = strings.TrimSpace(value)
	}

	records, err := s.ListThemes()
	if err != nil {
		log.Printf("[engram] ignoring the stored themes: %v", err)
		return selection
	}
	documents := make(map[string][]byte, len(records))
	for _, record := range records {
		documents[record.Name] = record.Palette
	}
	stored, unreadable := theme.PalettesFromDocuments(documents)
	for _, name := range unreadable {
		fmt.Fprintf(os.Stderr, "engram: theme %q is not a readable theme document, ignoring it\n", name)
	}
	selection.Stored = stored
	return selection
}

// tuiConfigFile is the shape of <data-dir>/config.json's "tui" section.
// The TUI calls this file "~/.engram/config.json"; cfg.DataDir is
// engram's home directory (ENGRAM_DATA_DIR-overridable, cfg.DataDir is
// ~/.engram by default per store.DefaultConfig), so reading
// filepath.Join(cfg.DataDir, "config.json") is the same file under that
// name.
//
// This is a different, new file from the per-project ".engram/config.json"
// internal/project.DetectProjectFull already reads (a repo-relative lock
// carrying only project_name): that one is scoped to a single repository
// and does not exist at cfg.DataDir, and this task does not repurpose it —
// tui.theme is a genuinely new global setting with nowhere else to live.
type tuiConfigFile struct {
	TUI struct {
		Theme string `json:"theme"`
	} `json:"tui"`
}

// readTUIThemeConfig reads tui.theme from <data-dir>/config.json. The file
// not existing is the common case (no such global config has ever been
// needed before this task) and is not an error; a malformed file is logged
// and otherwise ignored rather than fatal, since a broken optional config
// file should not block `engram tui` from starting.
func readTUIThemeConfig(cfg store.Config) string {
	data, err := os.ReadFile(filepath.Join(cfg.DataDir, "config.json"))
	if err != nil {
		return ""
	}
	var parsed tuiConfigFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		log.Printf("[engram] ignoring %s: %v", filepath.Join(cfg.DataDir, "config.json"), err)
		return ""
	}
	return strings.TrimSpace(parsed.TUI.Theme)
}

func cmdSearch(cfg store.Config) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: engram search <query> [--type TYPE] [--project PROJECT] [--scope SCOPE] [--limit N]")
		exitFunc(1)
	}

	// Collect the query (everything that's not a flag)
	var queryParts []string
	opts := store.SearchOptions{Limit: 10}

	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--type":
			if i+1 < len(os.Args) {
				opts.Type = os.Args[i+1]
				i++
			}
		case "--project":
			if i+1 < len(os.Args) {
				opts.Project = os.Args[i+1]
				i++
			}
		case "--limit":
			if i+1 < len(os.Args) {
				if n, err := strconv.Atoi(os.Args[i+1]); err == nil {
					opts.Limit = n
				}
				i++
			}
		case "--scope":
			if i+1 < len(os.Args) {
				opts.Scope = os.Args[i+1]
				i++
			}
		default:
			queryParts = append(queryParts, os.Args[i])
		}
	}

	query := strings.Join(queryParts, " ")
	if query == "" {
		fmt.Fprintln(os.Stderr, "error: search query is required")
		exitFunc(1)
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
		return
	}
	defer s.Close()

	results, err := storeSearch(s, query, opts)
	if err != nil {
		fatal(err)
		return
	}

	if len(results) == 0 {
		fmt.Printf("No memories found for: %q\n", query)
		return
	}

	fmt.Printf("Found %d memories:\n\n", len(results))
	for i, r := range results {
		project := ""
		if r.Project != nil {
			project = fmt.Sprintf(" | project: %s", *r.Project)
		}
		fmt.Printf("[%d] #%d (%s) — %s\n    %s\n    %s%s | scope: %s\n\n",
			i+1, r.ID, r.Type, r.Title,
			truncate(r.Content, 300),
			timeutil.FormatLocal(r.CreatedAt), project, r.Scope)
	}
}

func cmdSave(cfg store.Config) {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: engram save <title> <content> [--type TYPE] [--project PROJECT] [--scope SCOPE] [--topic TOPIC_KEY]")
		exitFunc(1)
	}

	title := os.Args[2]
	content := os.Args[3]
	typ := "manual"
	project := ""
	scope := "project"
	topicKey := ""

	for i := 4; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--type":
			if i+1 < len(os.Args) {
				typ = os.Args[i+1]
				i++
			}
		case "--project":
			if i+1 < len(os.Args) {
				project = os.Args[i+1]
				i++
			}
		case "--scope":
			if i+1 < len(os.Args) {
				scope = os.Args[i+1]
				i++
			}
		case "--topic":
			if i+1 < len(os.Args) {
				topicKey = os.Args[i+1]
				i++
			}
		}
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	sessionID := "manual-save"
	if project != "" {
		sessionID = "manual-save-" + project
	}
	cwd, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	if err := s.CreateSession(sessionID, project, cwd); err != nil {
		fatal(err)
	}
	id, err := storeAddObservation(s, store.AddObservationParams{
		SessionID: sessionID,
		Type:      typ,
		Title:     title,
		Content:   content,
		Project:   project,
		Scope:     scope,
		TopicKey:  topicKey,
	})
	if err != nil {
		fatal(err)
	}

	fmt.Printf("Memory saved: #%d %q (%s)\n", id, title, typ)
}

func cmdDelete(cfg store.Config) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: engram delete <observation_id> [--hard]")
		fmt.Fprintln(os.Stderr, "       engram delete session  <id>")
		fmt.Fprintln(os.Stderr, "       engram delete prompt   <id>")
		fmt.Fprintln(os.Stderr, "       engram delete project  <name> [--hard]")
		exitFunc(1)
		return
	}

	sub := os.Args[2]
	switch sub {
	case "session":
		cmdDeleteSession(cfg)
	case "prompt":
		cmdDeletePrompt(cfg)
	case "project":
		cmdDeleteProject(cfg)
	default:
		// Backward-compat: treat the second arg as a numeric observation ID.
		cmdDeleteObservation(cfg)
	}
}

func cmdDeleteObservation(cfg store.Config) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: engram delete <observation_id> [--hard]")
		exitFunc(1)
		return
	}

	id, err := strconv.ParseInt(os.Args[2], 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid observation id %q\n", os.Args[2])
		exitFunc(1)
		return
	}

	hard := false
	for i := 3; i < len(os.Args); i++ {
		if os.Args[i] == "--hard" {
			hard = true
		}
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
		return
	}
	defer s.Close()

	if err := storeDeleteObservation(s, id, hard); err != nil {
		fatal(err)
		return
	}

	kind := "soft-deleted"
	if hard {
		kind = "hard-deleted"
	}
	fmt.Printf("Observation #%d %s\n", id, kind)
}

func cmdDeleteSession(cfg store.Config) {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: engram delete session <id>")
		exitFunc(1)
		return
	}

	id := os.Args[3]

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
		return
	}
	defer s.Close()

	if err := storeDeleteSession(s, id); err != nil {
		fatal(err)
		return
	}
	fmt.Printf("Session %q deleted\n", id)
}

func cmdDeletePrompt(cfg store.Config) {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: engram delete prompt <id>")
		exitFunc(1)
		return
	}

	id, err := strconv.ParseInt(os.Args[3], 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid prompt id %q\n", os.Args[3])
		exitFunc(1)
		return
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
		return
	}
	defer s.Close()

	if err := storeDeletePrompt(s, id); err != nil {
		fatal(err)
		return
	}
	fmt.Printf("Prompt #%d deleted\n", id)
}

func cmdDeleteProject(cfg store.Config) {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: engram delete project <name> [--hard]")
		exitFunc(1)
		return
	}

	name := os.Args[3]
	hard := false
	for i := 4; i < len(os.Args); i++ {
		if os.Args[i] == "--hard" {
			hard = true
		}
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
		return
	}
	defer s.Close()

	result, err := storeDeleteProject(s, name, hard)
	if err != nil {
		fatal(err)
		return
	}

	kind := "soft-deleted"
	if hard {
		kind = "hard-deleted"
	}
	fmt.Printf("Project %q %s: %d observation(s), %d prompt(s), %d session(s)\n",
		result.Project, kind, result.ObservationsDeleted, result.PromptsDeleted, result.SessionsDeleted)
}

func cmdTimeline(cfg store.Config) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: engram timeline <observation_id> [--before N] [--after N]")
		exitFunc(1)
	}

	obsID, err := strconv.ParseInt(os.Args[2], 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid observation id %q\n", os.Args[2])
		exitFunc(1)
	}

	before, after := 5, 5
	for i := 3; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--before":
			if i+1 < len(os.Args) {
				if n, err := strconv.Atoi(os.Args[i+1]); err == nil {
					before = n
				}
				i++
			}
		case "--after":
			if i+1 < len(os.Args) {
				if n, err := strconv.Atoi(os.Args[i+1]); err == nil {
					after = n
				}
				i++
			}
		}
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	result, err := storeTimeline(s, obsID, before, after)
	if err != nil {
		fatal(err)
	}

	// Session header
	if result.SessionInfo != nil {
		summary := ""
		if result.SessionInfo.Summary != nil {
			summary = fmt.Sprintf(" — %s", truncate(*result.SessionInfo.Summary, 100))
		}
		fmt.Printf("Session: %s (%s)%s\n", result.SessionInfo.Project, result.SessionInfo.StartedAt, summary)
		fmt.Printf("Total observations in session: %d\n\n", result.TotalInRange)
	}

	// Before
	if len(result.Before) > 0 {
		fmt.Println("─── Before ───")
		for _, e := range result.Before {
			fmt.Printf("  #%d [%s] %s — %s\n", e.ID, e.Type, e.Title, truncate(e.Content, 150))
		}
		fmt.Println()
	}

	// Focus
	fmt.Printf(">>> #%d [%s] %s <<<\n", result.Focus.ID, result.Focus.Type, result.Focus.Title)
	fmt.Printf("    %s\n", truncate(result.Focus.Content, 500))
	fmt.Printf("    %s\n\n", timeutil.FormatLocal(result.Focus.CreatedAt))

	// After
	if len(result.After) > 0 {
		fmt.Println("─── After ───")
		for _, e := range result.After {
			fmt.Printf("  #%d [%s] %s — %s\n", e.ID, e.Type, e.Title, truncate(e.Content, 150))
		}
	}
}

func cmdContext(cfg store.Config) {
	project := ""
	scope := ""

	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--scope":
			if i+1 < len(os.Args) {
				scope = os.Args[i+1]
				i++
			}
		default:
			if project == "" {
				project = os.Args[i]
			}
		}
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	ctx, err := storeFormatContext(s, project, scope)
	if err != nil {
		fatal(err)
	}

	if ctx == "" {
		fmt.Println("No previous session memories found.")
		return
	}

	fmt.Print(ctx)
}

func cmdStats(cfg store.Config) {
	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	stats, err := storeStats(s)
	if err != nil {
		fatal(err)
	}

	projects := "none yet"
	if len(stats.Projects) > 0 {
		projects = strings.Join(stats.Projects, ", ")
	}

	fmt.Printf("Engram Memory Stats\n")
	fmt.Printf("  Sessions:     %d\n", stats.TotalSessions)
	fmt.Printf("  Observations: %d\n", stats.TotalObservations)
	fmt.Printf("  Prompts:      %d\n", stats.TotalPrompts)
	fmt.Printf("  Projects:     %s\n", projects)
	fmt.Printf("  Database:     %s/engram.db\n", cfg.DataDir)
}

func cmdExport(cfg store.Config) {
	outFile := "engram-export.json"
	if len(os.Args) > 2 {
		outFile = os.Args[2]
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	data, err := storeExport(s)
	if err != nil {
		fatal(err)
	}

	out, err := jsonMarshalIndent(data, "", "  ")
	if err != nil {
		fatal(err)
	}

	if err := os.WriteFile(outFile, out, 0644); err != nil {
		fatal(err)
	}

	fmt.Printf("Exported to %s\n", outFile)
	fmt.Printf("  Sessions:     %d\n", len(data.Sessions))
	fmt.Printf("  Observations: %d\n", len(data.Observations))
	fmt.Printf("  Prompts:      %d\n", len(data.Prompts))
}

func cmdImport(cfg store.Config) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: engram import <file.json>")
		exitFunc(1)
	}

	inFile := os.Args[2]
	raw, err := os.ReadFile(inFile)
	if err != nil {
		fatal(fmt.Errorf("read %s: %w", inFile, err))
	}

	var data store.ExportData
	if err := json.Unmarshal(raw, &data); err != nil {
		fatal(fmt.Errorf("parse %s: %w", inFile, err))
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	result, err := s.Import(&data)
	if err != nil {
		fatal(err)
	}

	fmt.Printf("Imported from %s\n", inFile)
	fmt.Printf("  Sessions:     %d\n", result.SessionsImported)
	fmt.Printf("  Observations: %d\n", result.ObservationsImported)
	fmt.Printf("  Prompts:      %d\n", result.PromptsImported)
}

func cmdSync(cfg store.Config) {
	// Parse flags
	doImport := false
	doStatus := false
	doAll := false
	doCloud := false
	project := ""
	projectProvided := false
	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--help", "-h", "help":
			printSyncUsage()
			return
		case "--import":
			doImport = true
		case "--status":
			doStatus = true
		case "--all":
			doAll = true
		case "--cloud":
			doCloud = true
		case "--project":
			if i+1 < len(os.Args) {
				project = os.Args[i+1]
				projectProvided = true
				i++
			}
		}
	}

	// Default project using git detection (so sync only exports
	// memories for THIS project, not everything in the global DB).
	// --all skips project filtering entirely — exports everything.
	//
	// Local export is the one operation below that actually uses project to
	// decide where data lands (it filters which memories go into the
	// exported chunk); --status and --import do not consume it at all for
	// local sync. So cwd detection failing here is remembered, not fataled
	// immediately: cloud sync already demands an explicit --project on its
	// own (below), and a plain --status/--import must not be refused over a
	// project value it was never going to use. Only the export path checks
	// projectResolutionErr before it would silently scope to a
	// directory-name guess.
	var projectResolutionErr error
	if !doAll && project == "" {
		if cwd, err := os.Getwd(); err == nil {
			det := detectProjectFull(cwd)
			switch {
			case det.Error != nil:
				projectResolutionErr = fmt.Errorf("cannot determine project for sync: %w (use --project or --all)", det.Error)
			case isGuessedProjectSource(det.Source):
				projectResolutionErr = fmt.Errorf("project is not resolvable from %q: only a directory-name guess was found; use --project, set ENGRAM_PROJECT, or --all", det.Path)
			default:
				project = det.Project
			}
		}
	}
	if project != "" {
		normalizedProject, warning := store.NormalizeProject(project)
		project = normalizedProject
		if warning != "" {
			fmt.Fprintln(os.Stderr, warning)
		}
	}

	syncDir := ".engram"

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	cloudEnabled := doCloud || envBool("ENGRAM_CLOUD_SYNC")
	if cloudEnabled {
		if doAll {
			fatal(fmt.Errorf("cloud sync requires a single explicit --project scope; --all is not supported"))
		}
		if !projectProvided || strings.TrimSpace(project) == "" {
			fatal(fmt.Errorf("cloud sync requires an explicit non-empty --project value"))
		}
	}
	cloudTargetKey := cloudTargetKeyForProject(project)
	var sy *engramsync.Syncer

	markCloudHealthy := func() {
		if !cloudEnabled {
			return
		}
		if err := s.MarkSyncHealthy(cloudTargetKey); err != nil {
			fatal(fmt.Errorf("cloud sync health update: %w", err))
		}
	}

	markCloudSyncOutcome := func() {
		if !cloudEnabled {
			return
		}
		hasPending, err := s.HasPendingSyncMutationsForProject(project)
		if err != nil {
			fatal(fmt.Errorf("cloud sync state update: %w", err))
		}
		pendingImports := 0
		remoteStatusVerified := false
		if _, _, pending, statusErr := syncStatus(sy); statusErr == nil {
			pendingImports = pending
			remoteStatusVerified = true
		}
		if hasPending || (remoteStatusVerified && pendingImports > 0) {
			if err := s.MarkSyncPending(cloudTargetKey); err != nil {
				fatal(fmt.Errorf("cloud sync pending-state update: %w", err))
			}
			return
		}
		if !remoteStatusVerified {
			return
		}
		markCloudHealthy()
	}

	sy = engramsync.NewLocal(s, syncDir)
	if cloudEnabled {
		cc, err := preflightCloudSync(s, cfg, project, !doStatus)
		if err != nil {
			fatal(err)
		}
		transport, err := remote.NewRemoteTransport(cc.ServerURL, cc.Token, project)
		if err != nil {
			if !doStatus {
				markCloudSyncFailure(s, cloudTargetKey, err)
			}
			fatal(errors.New(cloudSyncFailureMessage(project, err)))
		}
		sy = engramsync.NewCloudWithTransport(s, transport, project)
	}

	if doStatus {
		local, remote, pending, err := syncStatus(sy)
		if err != nil {
			fatal(err)
		}
		if cloudEnabled {
			fmt.Printf("Cloud sync status (project=%q):\n", project)
			fmt.Printf("  Local chunks:    %d\n", local)
			fmt.Printf("  Remote chunks:   %d\n", remote)
			fmt.Printf("  Pending import:  %d\n", pending)
			return
		}
		fmt.Printf("Sync status:\n")
		fmt.Printf("  Local chunks:    %d\n", local)
		fmt.Printf("  Remote chunks:   %d\n", remote)
		fmt.Printf("  Pending import:  %d\n", pending)
		return
	}

	if doImport {
		result, err := syncImport(sy)
		if err != nil {
			if cloudEnabled {
				markCloudSyncFailure(s, cloudTargetKey, err)
			}
			if cloudEnabled {
				fatal(errors.New(cloudSyncFailureMessage(project, err)))
			}
			fatal(err)
		}
		markCloudSyncOutcome()

		if result.ChunksImported == 0 {
			fmt.Println("No new chunks to import.")
			if result.ChunksSkipped > 0 {
				fmt.Printf("  (%d chunks already imported)\n", result.ChunksSkipped)
			}
			return
		}

		if cloudEnabled {
			fmt.Printf("Imported %d new remote chunk(s) for project %q\n", result.ChunksImported, project)
		} else {
			fmt.Printf("Imported %d new chunk(s) from .engram/\n", result.ChunksImported)
		}
		fmt.Printf("  Sessions:     %d\n", result.SessionsImported)
		fmt.Printf("  Observations: %d\n", result.ObservationsImported)
		fmt.Printf("  Prompts:      %d\n", result.PromptsImported)
		if result.ChunksSkipped > 0 {
			fmt.Printf("  Skipped:      %d (already imported)\n", result.ChunksSkipped)
		}
		return
	}

	// Export: DB → new chunk. This is the operation project actually scopes
	// for local sync, so a detection failure remembered above is fatal here
	// and not before (see the comment where projectResolutionErr is set).
	if !doAll && projectResolutionErr != nil {
		fatal(projectResolutionErr)
	}
	username := engramsync.GetUsername()
	if doAll {
		fmt.Println("Exporting ALL memories (all projects)...")
	} else {
		if cloudEnabled {
			fmt.Printf("Exporting memories for project %q to cloud...\n", project)
		} else {
			fmt.Printf("Exporting memories for project %q...\n", project)
		}
	}
	result, err := syncExport(sy, username, project)
	if err != nil {
		if cloudEnabled {
			markCloudSyncFailure(s, cloudTargetKey, err)
			fatal(errors.New(cloudSyncFailureMessage(project, err)))
		}
		fatal(err)
	}
	markCloudSyncOutcome()

	if result.IsEmpty {
		if doAll {
			fmt.Println("Nothing new to sync — all memories already exported.")
		} else {
			fmt.Printf("Nothing new to sync for project %q — all memories already exported.\n", project)
		}
		return
	}

	fmt.Printf("Created chunk %s\n", result.ChunkID)
	fmt.Printf("  Sessions:     %d\n", result.SessionsExported)
	fmt.Printf("  Observations: %d\n", result.ObservationsExported)
	fmt.Printf("  Prompts:      %d\n", result.PromptsExported)
	if result.MutationsExported > 0 {
		fmt.Printf("  Mutations:    %d\n", result.MutationsExported)
	}
	if cloudEnabled {
		fmt.Printf("Cloud sync complete for project %q.\n", project)
		return
	}
	fmt.Println()
	fmt.Println("Add to git:")
	fmt.Printf("  git add .engram/ && git commit -m \"sync engram memories\"\n")
}

func printSyncUsage() {
	fmt.Println("usage: engram sync [--import | --status] [--all] [--cloud --project PROJECT]")
	fmt.Println("Local sync exports project-scoped chunks to .engram/ by default.")
	fmt.Println("Cloud sync requires an explicit --project and never runs from --help.")
}

// storeAdapter wraps *store.Store to satisfy obsidian.StoreReader.
// The real store.Stats() returns (*store.Stats, error); the interface expects *store.Stats.
type storeAdapter struct{ s *store.Store }

func (a *storeAdapter) Export() (*store.ExportData, error) { return a.s.Export() }
func (a *storeAdapter) Stats() *store.Stats {
	st, _ := a.s.Stats()
	return st
}

// ObservationExportRefs forwards the optional engram-projects capability of
// the exporter (RFC §9.5). The adapter has to declare it explicitly: the
// exporter type-asserts on what it is handed, so an adapter that forwards
// only Export/Stats silently exports notes without their knowledge_ref,
// jira_key, runbook_id and graph_commit — and nothing fails while it does.
func (a *storeAdapter) ObservationExportRefs(project string) (map[string]store.ObservationExportRefs, error) {
	return a.s.ObservationExportRefs(project)
}

// Compile-time guard for the paragraph above.
var _ obsidian.ProjectRefReader = (*storeAdapter)(nil)

func cmdObsidianExport(cfg store.Config) {
	// Parse flags
	var (
		vault       string
		project     string
		limit       int
		since       string
		force       bool
		graphConfig string
		watch       bool
		interval    string
	)

	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--vault":
			if i+1 < len(os.Args) {
				vault = os.Args[i+1]
				i++
			}
		case "--project":
			if i+1 < len(os.Args) {
				project = os.Args[i+1]
				i++
			}
		case "--limit":
			if i+1 < len(os.Args) {
				if n, err := strconv.Atoi(os.Args[i+1]); err == nil {
					limit = n
				}
				i++
			}
		case "--since":
			if i+1 < len(os.Args) {
				since = os.Args[i+1]
				i++
			}
		case "--force":
			force = true
		case "--graph-config":
			if i+1 < len(os.Args) {
				graphConfig = os.Args[i+1]
				i++
			}
		case "--watch":
			watch = true
		case "--interval":
			if i+1 < len(os.Args) {
				interval = os.Args[i+1]
				i++
			}
		default:
			fmt.Fprintf(os.Stderr, "engram: unknown flag: %s\n", os.Args[i])
			exitFunc(1)
		}
	}

	if vault == "" {
		fmt.Fprintln(os.Stderr, "error: flag --vault is required")
		exitFunc(1)
	}

	// Default --graph-config to "preserve"
	if graphConfig == "" {
		graphConfig = string(obsidian.GraphConfigPreserve)
	}

	graphMode, err := obsidian.ParseGraphConfigMode(graphConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid --graph-config value: %s (accepted: preserve, force, skip)\n", graphConfig)
		exitFunc(1)
	}

	// Validate --interval requires --watch
	if interval != "" && !watch {
		fmt.Fprintln(os.Stderr, "error: --interval requires --watch")
		exitFunc(1)
	}

	// Parse and validate --interval (default 10m when --watch is set)
	var watchInterval time.Duration
	if watch {
		intervalStr := interval
		if intervalStr == "" {
			intervalStr = "10m"
		}
		d, parseErr := time.ParseDuration(intervalStr)
		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "error: invalid --interval value %q: %v\n", intervalStr, parseErr)
			exitFunc(1)
		}
		if d < time.Minute {
			fmt.Fprintf(os.Stderr, "error: --interval must be at least 1m (minimum), got %v\n", d)
			exitFunc(1)
		}
		watchInterval = d
	}

	exportCfg := obsidian.ExportConfig{
		VaultPath:   vault,
		Project:     project,
		Limit:       limit,
		Force:       force,
		GraphConfig: graphMode,
	}

	if since != "" {
		// Try common date formats: full RFC3339, date-only (YYYY-MM-DD)
		var sinceTime time.Time
		var parseErr error
		for _, layout := range []string{time.RFC3339, "2006-01-02"} {
			sinceTime, parseErr = time.Parse(layout, since)
			if parseErr == nil {
				break
			}
		}
		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "error: invalid --since value %q (expected YYYY-MM-DD or RFC3339)\n", since)
			exitFunc(1)
		}
		exportCfg.Since = sinceTime
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	exp := newObsidianExporter(&storeAdapter{s: s}, exportCfg)

	if watch {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		w := newObsidianWatcher(obsidian.WatcherConfig{
			Exporter: exp,
			Interval: watchInterval,
			Logf:     log.Printf,
		})

		if w != nil {
			if runErr := w.Run(ctx); runErr != nil {
				log.Printf("[engram] shutting down watch mode: %v", runErr)
			} else {
				log.Printf("[engram] shutting down watch mode")
			}
		}
		exitFunc(0)
		return
	}

	result, err := exp.Export()
	if err != nil {
		fatal(err)
	}

	fmt.Printf("Obsidian export complete\n")
	fmt.Printf("  Created: %d\n", result.Created)
	fmt.Printf("  Updated: %d\n", result.Updated)
	fmt.Printf("  Deleted: %d\n", result.Deleted)
	fmt.Printf("  Skipped: %d\n", result.Skipped)
	fmt.Printf("  Hubs:    %d\n", result.HubsCreated)
	if len(result.Errors) > 0 {
		fmt.Fprintf(os.Stderr, "  Errors: %d\n", len(result.Errors))
		for _, e := range result.Errors {
			fmt.Fprintf(os.Stderr, "    - %v\n", e)
		}
	}
}

func cmdProjects(cfg store.Config) {
	// Route: engram projects list | engram projects consolidate [--all] [--dry-run]
	subCmd := "list"
	if len(os.Args) > 2 {
		subCmd = os.Args[2]
	}
	switch subCmd {
	case "consolidate":
		cmdProjectsConsolidate(cfg)
	case "prune":
		cmdProjectsPrune(cfg)
	case "merge":
		cmdProjectsMerge(cfg)
	case "list", "":
		cmdProjectsList(cfg)
	default:
		fmt.Fprintf(os.Stderr, "unknown projects subcommand: %s\n", subCmd)
		fmt.Fprintln(os.Stderr, "usage: engram projects list [--json]")
		fmt.Fprintln(os.Stderr, "       engram projects merge <from>[,<from>…] <to> [--json]")
		fmt.Fprintln(os.Stderr, "       engram projects consolidate [--all] [--dry-run]")
		fmt.Fprintln(os.Stderr, "       engram projects prune [--dry-run]")
		exitFunc(1)
	}
}

// projectsSubcommandArgs returns what follows `engram projects <subcommand>`.
// `engram projects` on its own routes to list with no subcommand word at all,
// so the slice has to be taken defensively rather than assumed to exist.
func projectsSubcommandArgs() []string {
	if len(os.Args) <= 3 {
		return nil
	}
	return os.Args[3:]
}

// cmdProjectsMerge implements `engram projects merge <from> <to>`: the command
// the alias refusal and the CLI reference both send a reader to, over the same
// store call the mem_merge_projects MCP tool makes. Without it the only way to
// collapse two names for one project is the MCP surface, which a shell script
// rolling out a reorganisation does not have.
//
// <from> takes one name or a comma-separated list, matching what the MCP tool
// accepts, so a cluster of names that drifted apart collapses in a single
// transaction instead of one call per name.
func cmdProjectsMerge(cfg store.Config) {
	positional, rest := projSplitPositional(projectsSubcommandArgs(), 2)
	f := projNewFlags("engram projects merge")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	if !f.parse(rest) {
		return
	}
	if len(positional) < 2 {
		projFail(*jsonOut, "missing_field",
			"usage: engram projects merge <from>[,<from>…] <to>", nil)
		return
	}

	// Sources keep the bytes the caller typed: the "project" column is
	// case-sensitive, so "Pipas" and "pipas" are two distinct sets of rows
	// and normalizing a source here would ask the store to move the wrong
	// one. The canonical name is normalized because that is what every
	// write path stores.
	var sources []string
	for _, raw := range strings.Split(positional[0], ",") {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			sources = append(sources, trimmed)
		}
	}
	target, _ := store.NormalizeProject(positional[1])
	if len(sources) == 0 {
		projFail(*jsonOut, "missing_field", "no source project given", nil)
		return
	}
	if target == "" {
		projFail(*jsonOut, "invalid_slug", fmt.Sprintf("invalid target project %q", positional[1]), nil)
		return
	}
	for _, src := range sources {
		if src == target {
			projFail(*jsonOut, "merge_into_self",
				fmt.Sprintf("source %q is the target project; there is nothing to merge", src), nil)
			return
		}
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
		return
	}
	defer s.Close()

	// A source the store has never seen is a typo, not an empty merge: the
	// store would report "no matching records found" and exit 0, which a
	// rollout script reads as success.
	for _, src := range sources {
		normalized, _ := store.NormalizeProject(src)
		if !projBackedProject(s, normalized) {
			projFail(*jsonOut, "unknown_project",
				fmt.Sprintf("source project %q is not backed by a card or memories", src), nil)
			return
		}
	}

	result, err := s.MergeProjects(sources, target)
	if err != nil {
		projFail(*jsonOut, "merge_failed", err.Error(), nil)
		return
	}

	projPrintResult(*jsonOut, projScope{Slug: target, Source: project.SourceExplicitOverride}, result, func() {
		fmt.Printf("Merged %d source(s) into %q:\n", len(result.SourcesMerged), result.Canonical)
		for _, line := range result.TableSummaryLines() {
			fmt.Printf("  %s\n", line)
		}
		for _, line := range result.SourcesSkippedLines() {
			fmt.Printf("  skipped %s\n", line)
		}
	})
}

// projectListCounts is the `counts` object of one `projects list --json` row.
type projectListCounts struct {
	Observations int `json:"observations"`
	Sessions     int `json:"sessions"`
	Prompts      int `json:"prompts"`
}

// projectListRow is one entry of the `projects list --json` array. Everything
// below Name comes from the project card and is absent for a project that has
// none, so a consumer can tell "no card" from "a card that says nothing".
type projectListRow struct {
	Name        string            `json:"name"`
	Slug        string            `json:"slug,omitempty"`
	DisplayName string            `json:"display_name,omitempty"`
	Parent      *string           `json:"parent,omitempty"`
	Kind        string            `json:"kind,omitempty"`
	Counts      projectListCounts `json:"counts"`
	Directories []string          `json:"directories,omitempty"`
}

func cmdProjectsList(cfg store.Config) {
	jsonOut := false
	for _, arg := range projectsSubcommandArgs() {
		if arg == "--json" {
			jsonOut = true
		}
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	projects, err := s.ListProjectsWithStats()
	if err != nil {
		fatal(err)
	}

	if jsonOut {
		printProjectsListJSON(s, projects)
		return
	}

	if len(projects) == 0 {
		fmt.Println("No projects found.")
		return
	}

	fmt.Printf("Projects (%d):\n", len(projects))
	for _, p := range projects {
		sessionWord := "sessions"
		if p.SessionCount == 1 {
			sessionWord = "session"
		}
		promptWord := "prompts"
		if p.PromptCount == 1 {
			promptWord = "prompt"
		}
		fmt.Printf("  %-30s %4d obs   %3d %-9s  %3d %s\n",
			p.Name,
			p.ObservationCount,
			p.SessionCount, sessionWord,
			p.PromptCount, promptWord,
		)
	}
}

// printProjectsListJSON writes the array `projects list --json` promises: one
// object per project, in the order the table renders them, each carrying its
// card identity when it has a card. It is an array rather than the envelope
// the singular commands print because no single project is in scope here.
//
// An empty store prints `[]`, not prose: a script has to be able to read the
// answer without branching on a sentence.
func printProjectsListJSON(s *store.Store, projects []store.ProjectStats) {
	cards, _, err := s.ListProjectCards(false)
	if err != nil {
		fatal(err)
		return
	}
	bySlug := make(map[string]store.ProjectCardListItem, len(cards))
	for _, card := range cards {
		bySlug[card.Slug] = card
	}

	rows := make([]projectListRow, 0, len(projects))
	for _, p := range projects {
		row := projectListRow{
			Name: p.Name,
			Counts: projectListCounts{
				Observations: p.ObservationCount,
				Sessions:     p.SessionCount,
				Prompts:      p.PromptCount,
			},
			Directories: p.Directories,
		}
		slug, _ := store.NormalizeProject(p.Name)
		if card, ok := bySlug[slug]; ok {
			row.Slug = card.Slug
			row.DisplayName = card.DisplayName
			row.Parent = card.ParentSlug
			row.Kind = card.Kind
		}
		rows = append(rows, row)
	}

	out, err := jsonMarshalIndent(rows, "", "  ")
	if err != nil {
		fatal(err)
		return
	}
	fmt.Fprintln(os.Stdout, string(out))
}

// projectGroup represents a set of project names that should be merged.
type projectGroup struct {
	Names     []string
	Canonical string // suggested canonical (most observations)
}

// groupSimilarProjects groups projects by name similarity and shared directories.
// Uses a simple union-find approach.
func groupSimilarProjects(projects []store.ProjectStats) []projectGroup {
	n := len(projects)
	if n == 0 {
		return nil
	}

	// parent[i] holds the root of i's component
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}

	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(x, y int) {
		rx, ry := find(x), find(y)
		if rx != ry {
			parent[rx] = ry
		}
	}

	// Build name-only slice and index map for FindSimilar
	names := make([]string, n)
	nameToIndex := make(map[string]int, n)
	for i, p := range projects {
		names[i] = p.Name
		nameToIndex[p.Name] = i
	}

	// Group by name similarity
	for i := 0; i < n; i++ {
		similar := project.FindSimilar(projects[i].Name, names, 3)
		for _, sm := range similar {
			if j, ok := nameToIndex[sm.Name]; ok {
				union(i, j)
			}
		}
	}

	// Group by shared directory
	dirToProjects := make(map[string][]int)
	for i, p := range projects {
		for _, dir := range p.Directories {
			if dir != "" {
				dirToProjects[dir] = append(dirToProjects[dir], i)
			}
		}
	}
	for _, idxs := range dirToProjects {
		for k := 1; k < len(idxs); k++ {
			union(idxs[0], idxs[k])
		}
	}

	// Collect components
	components := make(map[int][]int)
	for i := 0; i < n; i++ {
		root := find(i)
		components[root] = append(components[root], i)
	}

	// Build groups — skip singletons (no duplicates)
	var groups []projectGroup
	for _, idxs := range components {
		if len(idxs) < 2 {
			continue
		}
		// Suggest the one with most observations as canonical
		bestIdx := idxs[0]
		for _, idx := range idxs[1:] {
			if projects[idx].ObservationCount > projects[bestIdx].ObservationCount {
				bestIdx = idx
			}
		}
		grpNames := make([]string, len(idxs))
		for k, idx := range idxs {
			grpNames[k] = projects[idx].Name
		}
		sort.Strings(grpNames)
		groups = append(groups, projectGroup{
			Names:     grpNames,
			Canonical: projects[bestIdx].Name,
		})
	}
	// Sort groups by canonical name for deterministic output
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Canonical < groups[j].Canonical
	})
	return groups
}

func cmdProjectsConsolidate(cfg store.Config) {
	doAll := false
	dryRun := false
	for i := 3; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--all":
			doAll = true
		case "--dry-run":
			dryRun = true
		}
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	if !doAll {
		// Single-project mode: detect canonical project for cwd, find variants.
		// Consolidation decides where a whole set of existing memories lands
		// (everything similar to "canonical" is merged into it), so cwd
		// detection must be backed by a fact — a directory-name guess is
		// refused instead of silently naming the merge target.
		cwd, err := os.Getwd()
		if err != nil {
			fatal(err)
		}
		det := detectProjectFull(cwd)
		if det.Error != nil {
			fatal(fmt.Errorf("cannot determine the canonical project to consolidate: %w (use --all, or run from a resolvable project directory)", det.Error))
		}
		if isGuessedProjectSource(det.Source) {
			fatal(fmt.Errorf("project is not resolvable from %q: only a directory-name guess was found; run from inside a git repository, or use --all", det.Path))
		}
		canonical := det.Project

		allNames, err := s.ListProjectNames()
		if err != nil {
			fatal(err)
		}

		// Check if the detected canonical actually exists in the DB.
		canonicalExists := false
		for _, n := range allNames {
			if n == canonical {
				canonicalExists = true
				break
			}
		}
		if !canonicalExists {
			fmt.Printf("Note: %q has no existing memories. Merging will move memories into this new project name.\n", canonical)
		}

		// Find candidates by name similarity
		similar := project.FindSimilar(canonical, allNames, 3)

		// Also find candidates by shared directory (catches renames like sdd-agent-team → agent-teams-lite)
		allStats, _ := s.ListProjectsWithStats()
		statsMap := make(map[string]store.ProjectStats)
		var cwdDirs []string // directories for the canonical project
		for _, ps := range allStats {
			statsMap[ps.Name] = ps
			if ps.Name == canonical {
				cwdDirs = ps.Directories
			}
		}
		// If canonical has no stats yet, use cwd as its directory
		if len(cwdDirs) == 0 {
			cwdDirs = []string{cwd}
		}
		// Find projects sharing a directory with the canonical
		similarNames := make(map[string]bool)
		for _, sm := range similar {
			similarNames[sm.Name] = true
		}
		for _, ps := range allStats {
			if ps.Name == canonical || similarNames[ps.Name] {
				continue
			}
			for _, d := range ps.Directories {
				for _, cd := range cwdDirs {
					if d == cd {
						similar = append(similar, project.ProjectMatch{
							Name:      ps.Name,
							MatchType: "shared directory",
						})
						similarNames[ps.Name] = true
					}
				}
			}
		}

		if len(similar) == 0 {
			fmt.Printf("No similar project names found for %q. Nothing to consolidate.\n", canonical)
			return
		}

		fmt.Printf("Detected project: %q\n\n", canonical)
		fmt.Printf("Found similar project names:\n")
		for i, sm := range similar {
			obs := 0
			if ps, ok := statsMap[sm.Name]; ok {
				obs = ps.ObservationCount
			}
			fmt.Printf("  [%d] %-30s %3d obs  (%s)\n", i+1, sm.Name, obs, sm.MatchType)
		}

		if dryRun {
			fmt.Printf("\n[dry-run] Would merge %d project(s) into %q\n", len(similar), canonical)
			return
		}

		fmt.Printf("\nSelect which to merge into %q (comma-separated numbers, 'all', or 'none'): ", canonical)
		var answer string
		scanInputLine(&answer)
		answer = strings.TrimSpace(strings.ToLower(answer))

		if answer == "none" || answer == "n" || answer == "" {
			fmt.Println("Cancelled.")
			return
		}

		var sources []string
		if answer == "all" || answer == "a" {
			for _, sm := range similar {
				sources = append(sources, sm.Name)
			}
		} else {
			// Parse comma-separated indices
			for _, part := range strings.Split(answer, ",") {
				part = strings.TrimSpace(part)
				idx := 0
				if _, err := fmt.Sscanf(part, "%d", &idx); err != nil || idx < 1 || idx > len(similar) {
					fmt.Fprintf(os.Stderr, "Invalid selection: %q (expected 1-%d)\n", part, len(similar))
					return
				}
				sources = append(sources, similar[idx-1].Name)
			}
		}

		if len(sources) == 0 {
			fmt.Println("Nothing selected.")
			return
		}

		fmt.Printf("\nMerging %d project(s) into %q...\n", len(sources), canonical)
		result, err := s.MergeProjects(sources, canonical)
		if err != nil {
			fatal(err)
		}

		fmt.Printf("Done! Merged into %q:\n", result.Canonical)
		for _, line := range result.TableSummaryLines() {
			fmt.Printf("  %s\n", line)
		}
		for _, line := range result.SourcesSkippedLines() {
			fmt.Printf("  skipped %s\n", line)
		}
		return
	}

	// --all mode: group all projects by similarity + shared directories
	projects, err := s.ListProjectsWithStats()
	if err != nil {
		fatal(err)
	}

	groups := groupSimilarProjects(projects)

	if len(groups) == 0 {
		fmt.Println("No similar project name groups found.")
		return
	}

	fmt.Printf("Found %d group(s) of similar project names:\n\n", len(groups))

	// Build stats map for obs counts
	projectStatsMap := make(map[string]store.ProjectStats)
	for _, p := range projects {
		projectStatsMap[p.Name] = p
	}

	for i, g := range groups {
		fmt.Printf("Group %d:\n", i+1)
		for j, name := range g.Names {
			obs := 0
			if ps, ok := projectStatsMap[name]; ok {
				obs = ps.ObservationCount
			}
			marker := "  "
			if name == g.Canonical {
				marker = "→ "
			}
			fmt.Printf("  %s[%d] %-30s %3d obs\n", marker, j+1, name, obs)
		}
		fmt.Printf("  Suggested canonical: %q (→)\n", g.Canonical)

		if dryRun {
			fmt.Printf("  [dry-run] Would merge into %q\n\n", g.Canonical)
			continue
		}

		fmt.Printf("\n  Options:\n")
		fmt.Printf("    all     — merge everything into %q\n", g.Canonical)
		fmt.Printf("    1,3,... — merge only selected numbers into %q\n", g.Canonical)
		fmt.Printf("    rename  — choose a different canonical name\n")
		fmt.Printf("    skip    — don't touch this group\n")
		fmt.Printf("  Choice: ")
		var answer string
		scanInputLine(&answer)
		answer = strings.TrimSpace(strings.ToLower(answer))

		canonical := g.Canonical

		if answer == "skip" || answer == "s" || answer == "n" || answer == "" {
			fmt.Println("  Skipped.")
			fmt.Println()
			continue
		}

		if answer == "rename" || answer == "r" {
			fmt.Printf("  Enter canonical name: ")
			scanInputLine(&canonical)
			canonical = strings.TrimSpace(canonical)
			if canonical == "" {
				fmt.Println("  Empty input, skipping.")
				fmt.Println()
				continue
			}
			answer = "all" // after rename, merge everything into the new name
		}

		// Determine which sources to merge
		var sources []string
		if answer == "all" || answer == "a" || answer == "y" || answer == "yes" {
			for _, name := range g.Names {
				if name != canonical {
					sources = append(sources, name)
				}
			}
		} else {
			// Parse comma-separated indices
			for _, part := range strings.Split(answer, ",") {
				part = strings.TrimSpace(part)
				idx := 0
				if _, err := fmt.Sscanf(part, "%d", &idx); err != nil || idx < 1 || idx > len(g.Names) {
					fmt.Fprintf(os.Stderr, "  Invalid selection: %q (expected 1-%d)\n", part, len(g.Names))
					fmt.Println()
					continue
				}
				selected := g.Names[idx-1]
				if selected != canonical {
					sources = append(sources, selected)
				}
			}
		}
		if len(sources) == 0 {
			fmt.Println("  Nothing to merge.")
			fmt.Println()
			continue
		}

		result, err := s.MergeProjects(sources, canonical)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Error merging: %v\n", err)
			fmt.Println()
			continue
		}
		fmt.Printf("  Merged:\n")
		for _, line := range result.TableSummaryLines() {
			fmt.Printf("    %s\n", line)
		}
		for _, line := range result.SourcesSkippedLines() {
			fmt.Printf("    skipped %s\n", line)
		}
		fmt.Println()
	}
}

func cmdProjectsPrune(cfg store.Config) {
	dryRun := false
	for i := 3; i < len(os.Args); i++ {
		if os.Args[i] == "--dry-run" {
			dryRun = true
		}
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	allStats, err := s.ListProjectsWithStats()
	if err != nil {
		fatal(err)
	}

	// Find projects with 0 observations
	var candidates []store.ProjectStats
	for _, ps := range allStats {
		if ps.ObservationCount == 0 {
			candidates = append(candidates, ps)
		}
	}

	if len(candidates) == 0 {
		fmt.Println("No empty projects to prune.")
		return
	}

	fmt.Printf("Found %d project(s) with 0 observations:\n\n", len(candidates))
	for i, ps := range candidates {
		fmt.Printf("  [%d] %-30s %3d sessions  %3d prompts\n", i+1, ps.Name, ps.SessionCount, ps.PromptCount)
	}

	if dryRun {
		fmt.Printf("\n[dry-run] Would prune %d project(s)\n", len(candidates))
		return
	}

	fmt.Printf("\nSelect which to prune (comma-separated numbers, 'all', or 'none'): ")
	var answer string
	scanInputLine(&answer)
	answer = strings.TrimSpace(strings.ToLower(answer))

	if answer == "none" || answer == "n" || answer == "" {
		fmt.Println("Cancelled.")
		return
	}

	var selected []store.ProjectStats
	if answer == "all" || answer == "a" {
		selected = candidates
	} else {
		for _, part := range strings.Split(answer, ",") {
			part = strings.TrimSpace(part)
			idx := 0
			if _, err := fmt.Sscanf(part, "%d", &idx); err != nil || idx < 1 || idx > len(candidates) {
				fmt.Fprintf(os.Stderr, "Invalid selection: %q (expected 1-%d)\n", part, len(candidates))
				return
			}
			selected = append(selected, candidates[idx-1])
		}
	}

	if len(selected) == 0 {
		fmt.Println("Nothing selected.")
		return
	}

	totalSessions := int64(0)
	totalPrompts := int64(0)
	for _, ps := range selected {
		result, err := s.PruneProject(ps.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error pruning %q: %v\n", ps.Name, err)
			continue
		}
		totalSessions += result.SessionsDeleted
		totalPrompts += result.PromptsDeleted
	}

	fmt.Printf("\nPruned %d project(s): %d sessions, %d prompts removed.\n", len(selected), totalSessions, totalPrompts)
}

// cmdSetup classifies os.Args[2:] with a two-pass, order-independent
// algorithm (see openspec/changes/setup-protocol-flag/proposal.md,
// Approach; JD-014 residual fix). The FIRST pass scans every token and only
// accumulates classification state — it never dispatches mid-loop. This
// guarantees a token like --protocol=<v> is always parsed regardless of
// what precedes it (e.g. an earlier unrecognized hyphen-prefixed token no
// longer short-circuits the loop before later tokens are read). The SECOND
// pass dispatches once, in a fixed priority order, using the fully
// accumulated state: helpSeen > extraBareSeen > unknownFlagSeen > slug
// present > protocol-only > no args.
func cmdSetup(cfg store.Config) {
	args := os.Args[2:]

	var (
		helpSeen        bool
		protocolRaw     string
		protocolFlag    bool
		slug            string
		slugSeen        bool
		extraBareSeen   bool
		unknownFlagSeen bool
	)

	for i := 0; i < len(args); i++ {
		token := args[i]
		switch {
		case token == "--help" || token == "-h" || token == "help":
			helpSeen = true
		case token == "--protocol":
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				protocolRaw = args[i+1]
				i++
			} else {
				// Dangling --protocol: either it's the last token, or the
				// next token is itself a flag (e.g. `--protocol --help`).
				// Do NOT consume the next token as the value — leave it to
				// be classified normally on the next iteration so
				// `--protocol --help` still shows usage.
				protocolRaw = ""
			}
			protocolFlag = true
		case strings.HasPrefix(token, "--protocol="):
			protocolRaw = strings.TrimPrefix(token, "--protocol=")
			protocolFlag = true
		case strings.HasPrefix(token, "-"):
			// Unrecognized hyphen-prefixed token: record it but keep
			// scanning so a --protocol appearing later is still parsed
			// (JD-014 residual).
			unknownFlagSeen = true
		default:
			if slugSeen {
				extraBareSeen = true
			} else {
				slug = token
				slugSeen = true
			}
		}
	}

	switch {
	case helpSeen:
		printSetupUsage()
		return
	case extraBareSeen:
		fmt.Fprintln(os.Stderr, "usage: engram setup [<agent>] [--protocol=slim|full]")
		exitFunc(1)
		return
	case unknownFlagSeen:
		// Preserve the legacy fallback to the interactive menu (keeps
		// TestCmdSetupHyphenArgFallsBackToInteractive green), but forward
		// the already-parsed --protocol mode (if any) instead of dropping
		// it (JD-014), regardless of the unknown flag's position.
		mode := ""
		if protocolFlag {
			mode = resolveProtocolModeFlag(protocolRaw)
		}
		cmdSetupInteractive(cfg, mode)
		return
	case slugSeen:
		result, err := setupInstallAgent(slug)
		if err != nil {
			fatal(err)
		}
		if protocolFlag {
			applyProtocolMode(cfg, slug, resolveProtocolModeFlag(protocolRaw))
		}
		fmt.Printf("✓ Installed %s plugin (%d files)\n", result.Agent, result.Files)
		fmt.Printf("  → %s\n", result.Destination)
		printPostInstall(result)
	default:
		// No slug: interactive menu. Mode (if any) applies to whichever
		// slug the user selects.
		mode := ""
		if protocolFlag {
			mode = resolveProtocolModeFlag(protocolRaw)
		}
		cmdSetupInteractive(cfg, mode)
	}
}

// cmdSetupInteractive renders the agent picker and installs the chosen
// agent. mode is the already-resolved --protocol value ("slim"/"full") from
// a slug-less invocation, or "" when --protocol was not given at all.
func cmdSetupInteractive(cfg store.Config, mode string) {
	agents := setupSupportedAgents()

	fmt.Println("engram setup — Install agent plugin")
	fmt.Println()
	fmt.Println("Which agent do you want to set up?")
	fmt.Println()

	for i, a := range agents {
		fmt.Printf("  [%d] %s\n", i+1, a.Description)
		fmt.Printf("      Install to: %s\n\n", a.InstallDir)
	}

	fmt.Print("Enter choice (1-", len(agents), "): ")
	var input string
	scanInputLine(&input)

	choice, err := strconv.Atoi(strings.TrimSpace(input))
	if err != nil || choice < 1 || choice > len(agents) {
		fmt.Fprintln(os.Stderr, "Invalid choice.")
		exitFunc(1)
	}

	selected := agents[choice-1]
	fmt.Printf("\nInstalling %s plugin...\n", selected.Name)

	result, err := setupInstallAgent(selected.Name)
	if err != nil {
		fatal(err)
	}
	if mode != "" {
		applyProtocolMode(cfg, selected.Name, mode)
	}

	fmt.Printf("✓ Installed %s plugin (%d files)\n", result.Agent, result.Files)
	fmt.Printf("  → %s\n", result.Destination)
	printPostInstall(result)
}

// printSetupUsage prints `engram setup --help` output. Its Flags section
// MUST contain the literal "--protocol" (Guarantee 1); it must never read
// stdin (Guarantee 2 — safe under a detached/non-TTY stdin).
func printSetupUsage() {
	fmt.Println("usage: engram setup [<agent>] [--protocol=slim|full]")
	fmt.Println()
	fmt.Println("Install an agent plugin (claude-code, opencode, codex, ...).")
	fmt.Println("Without <agent>, shows an interactive menu.")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --protocol=<slim|full>  Set the session-start protocol verbosity for the")
	fmt.Println("                          installed agent slug (default: full). Unknown or")
	fmt.Println("                          missing values fall back to full with a warning.")
	fmt.Println("                          slim currently only takes effect for claude-code,")
	fmt.Println("                          and only when the installed engram is >= 1.4.0.")
	fmt.Println("  --help, -h              Show this help and exit.")
}

// resolveProtocolModeFlag normalizes a --protocol value to "slim" or "full".
// Unknown or empty values fall back to "full" with a non-fatal stderr
// warning — an invalid --protocol value never fails `engram setup`.
func resolveProtocolModeFlag(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case setup.ProtocolModeSlim:
		return setup.ProtocolModeSlim
	case setup.ProtocolModeFull:
		return setup.ProtocolModeFull
	default:
		fmt.Fprintf(os.Stderr, "warning: unknown --protocol value %q, defaulting to full\n", raw)
		return setup.ProtocolModeFull
	}
}

// applyProtocolMode persists the resolved protocol mode for slug, using the
// SAME cfg.DataDir main() resolved (ENGRAM_DATA_DIR override included) so the
// `protocol-mode` subcommand's read path matches this write path (JD-005). A
// write failure is reported as a non-fatal warning — it never fails setup.
func applyProtocolMode(cfg store.Config, slug, mode string) {
	if err := setup.WriteProtocolMode(cfg.DataDir, slug, mode); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not persist protocol mode: %v\n", err)
	}
}

// cmdProtocolMode implements `engram protocol-mode <slug>`: prints "slim" to
// stdout ONLY when the persisted mode for slug is "slim" AND the running
// binary's version meets the slim floor (>= 1.4.0); any other case
// (unrecognized slug, missing/corrupted mode file, version below floor,
// unparseable version) prints "full". All branching lives here in Go so it
// runs under `go test` — the Claude Code hook scripts only read this single
// line of stdout.
func cmdProtocolMode(cfg store.Config) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: engram protocol-mode <slug>")
		exitFunc(1)
		return
	}
	slug := os.Args[2]

	mode := setup.ReadProtocolMode(cfg.DataDir, slug)
	if mode == setup.ProtocolModeSlim && meetsProtocolVersionFloor(version) {
		fmt.Println(setup.ProtocolModeSlim)
		return
	}
	fmt.Println(setup.ProtocolModeFull)
}

// protocolVersionFloor is the minimum engram version required to honor a
// persisted "slim" protocol-mode: the slim status block relies on the
// MCP serverInstructions duplication fix shipped in this release.
var protocolVersionFloor = [3]int{1, 4, 0}

// meetsProtocolVersionFloor reports whether v (e.g. "1.4.0", "v1.5.2", or the
// build-time "dev" placeholder) is >= protocolVersionFloor. Any unparseable
// or empty value returns false — the caller then falls back to "full".
func meetsProtocolVersionFloor(v string) bool {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" || v == "dev" {
		return false
	}

	segments := strings.SplitN(v, ".", 3)
	var parts [3]int
	for i, s := range segments {
		if i >= 3 {
			break
		}
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return false
		}
		parts[i] = n
	}

	for i := 0; i < 3; i++ {
		if parts[i] != protocolVersionFloor[i] {
			return parts[i] > protocolVersionFloor[i]
		}
	}
	return true
}

func printPostInstall(result *setup.Result) {
	switch result.Agent {
	case "opencode":
		fmt.Println("\nNext steps:")
		fmt.Println("  1. Restart OpenCode — plugin + MCP server are ready")
		fmt.Println("  2. The plugin auto-starts the Engram HTTP server when needed")
		if result.TUIPluginEnabled {
			fmt.Println("\nAlso enabled: opencode-subagent-statusline in tui.json — sub-agent activity in the sidebar/footer.")
		}
	case "pi":
		fmt.Println("\nNext steps:")
		fmt.Println("  1. Restart Pi so packages and MCP config are reloaded")
		fmt.Println("  2. Verify with: pi list")
	case "claude-code":
		// Offer to add engram tools to the permissions allowlist
		fmt.Print("\nAdd engram tools to ~/.claude/settings.json allowlist?\n")
		fmt.Print("This prevents Claude Code from asking permission on every tool call.\n")
		fmt.Print("Add to allowlist? (y/N): ")
		var answer string
		scanInputLine(&answer)
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "y" || answer == "yes" {
			if err := setupAddClaudeCodeAllowlist(); err != nil {
				fmt.Fprintf(os.Stderr, "  warning: could not update allowlist: %v\n", err)
				fmt.Fprintln(os.Stderr, "  You can add them manually to permissions.allow in ~/.claude/settings.json")
			} else {
				fmt.Println("  ✓ Engram tools added to allowlist")
			}
		} else {
			fmt.Println("  Skipped. You can add them later to permissions.allow in ~/.claude/settings.json")
		}

		fmt.Println("\nNext steps:")
		fmt.Println("  1. Restart Claude Code — the plugin is active immediately")
		fmt.Println("  2. Verify with: claude plugin list")
		fmt.Println("  3. MCP config written to ~/.claude/mcp/engram.json using absolute binary path")
		fmt.Println("     (survives plugin auto-updates; re-run 'engram setup claude-code' if you move the binary)")
	default:
		// Every other agent's "next steps" are declared as data in the registry,
		// so the message is rendered generically instead of one case per agent.
		printNextSteps(setup.PostInstallSteps(result.Agent))
	}
}

// printNextSteps renders a numbered "Next steps" list, or nothing when empty.
func printNextSteps(steps []string) {
	if len(steps) == 0 {
		return
	}
	fmt.Println("\nNext steps:")
	for i, step := range steps {
		fmt.Printf("  %d. %s\n", i+1, step)
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func printUsage() {
	fmt.Printf(`engram v%s — Persistent memory for AI coding agents

Usage:
  engram <command> [arguments]

Commands:
  serve [port]       Start HTTP API server (default: 7437)
  mcp [--tools=PROFILE] [--project NAME]
                     Start MCP server (stdio transport, for any AI agent)
                       Profiles: agent (18 tools), admin (4 tools), projects (10 tools), all (default, 32)
                       Combine: --tools=agent,admin or pick individual tools
                       Example: engram mcp --tools=agent,projects
                       --project NAME  Set process-level default project (overrides cwd detection).
                                       Also accepted as ENGRAM_PROJECT=NAME env var.
  tui [--project NAME] [--theme NAME] [--no-mouse]
                     Launch the terminal workspace: eight tabs over the project's
                     memory, tasks, evidence, benchmarks, runbooks and graph.
                       --project NAME  Scope the workspace to a project. Without one it opens
                                       on the project tree, which ctrl+p reopens at any time.
                                       Also accepted as ENGRAM_PROJECT=NAME env var.
                       --theme NAME    Palette: koi-pond (default) | koi-day | showa | ogon |
                                       catppuccin-mocha | kanagawa | elephant. Run
                                       "engram theme list" for what this build ships.
                                       Also accepted as ENGRAM_TUI_THEME=NAME env var, or the
                                       tui.theme key in <data-dir>/config.json.
                       --no-mouse      Leave the mouse to the terminal, so a drag selects text
                                       the way it does everywhere else. Also settings['tui.mouse']
                                       = "off".
  search <query>     Search memories [--type TYPE] [--project PROJECT] [--scope SCOPE] [--limit N]
  save <title> <msg> Save a memory  [--type TYPE] [--project PROJECT] [--scope SCOPE]
  delete <obs_id>    Delete an observation [--hard] (soft-delete by default; --hard removes permanently)
  delete session <id>
                     Delete a session by ID (session must have no observations)
  delete prompt <id>
                     Delete a prompt by ID (permanent)
  delete project <name> [--hard]
                     Cascade-delete a project: soft-deletes observations (or hard if --hard),
                     removes prompts; with --hard also removes sessions
  timeline <obs_id>  Show chronological context around an observation [--before N] [--after N]
  conflicts <sub>   Inspect and manage memory conflict relations
                       list     [--project P]  [--status S]  [--since RFC3339]  [--limit N]
                       show     <relation_id>
                       stats    [--project P]
                       scan     [--project P]  [--since RFC3339]  [--dry-run]  [--apply]  [--max-insert N]
                                [--semantic]  [--concurrency N]  [--timeout-per-call SECONDS]
                                [--max-semantic N]  [--yes]
                       deferred [--status S]  [--limit N]  [--inspect SYNC_ID]  [--replay]
  doctor             Run read-only operational diagnostics [--json] [--project P] [--check CODE]
  context [project]  Show recent context from previous sessions
  stats              Show memory system statistics
  export [file]      Export all memories to JSON (default: engram-export.json)
  import <file>      Import memories from a JSON export file
  projects list      List all projects with observation, session, and prompt counts
  projects consolidate [--all] [--dry-run]
                     Merge similar project names into one canonical name
                       --all      Scan ALL projects for similar name groups
                       --dry-run  Preview what would be merged (no changes)
  project [<slug>] <sub>
                     engram-projects operations for one project (all accept --json)
                       card                     Pointers, counters and cloud sync status
                       upsert                   Create/update the project card
                       graph sync               Stamp graph_commit/graph_built_at/graph_summary
                       tasks list|upsert|link   Tasks and their linked observations
                       evidence add|list        Captured evidence files
                       runbooks sync|find       Runbook index and symptom search
                       context <task>           Compose a task's context pack
                       promote list|stamp       engram -> vault promotion candidates and knowledge_ref stamp
                       tree|set-parent|alias    The project tree and the names that redirect onto it
                       evidence scan <task>     Register what a task's vault folder holds
                       bench add|list|import    Measurements next to their baseline
                       import-vault [<root>]    Read a whole knowledge tree into the store
                       search <query>           Search the whole workspace at once
                     <slug> is optional: ENGRAM_PROJECT, then cwd detection.
                     Run "engram project help" for the full flag list.
  theme <sub>        Palettes the TUI renders with (all accept --json)
                       list                     Installed themes and which one is active
                       show <name>              Roles, gradient and contrast against both planes
                       use <name>               Remember the theme the TUI opens on
                       import <file> [--force]  Add a theme from a JSON document
                       export <name> [--out]    Write a theme as a JSON document
                       reset <name>             Put a builtin back the way it shipped
  setup [agent]      Install/setup agent integration (opencode, pi, claude-code,
                     gemini-cli, codex, antigravity-cli, windsurf, qwen, kiro,
                     cursor, vscode-copilot, kilocode)
  sync               Export new memories as compressed chunk to .engram/
                         --import   Import new chunks from .engram/ into local DB
                         --status   Show sync status
                         --project  Filter export to a specific project
                         --all      Export ALL projects (ignore directory-based filter)
		                 --cloud    Run sync against configured cloud endpoint (requires explicit --project)
	  cloud <subcommand> Cloud integration commands (opt-in)
	                        status     Show cloud config status
	                        enroll     Enroll a project for cloud sync
	                        config     Set cloud server URL
	                        serve      Run cloud backend + dashboard
  obsidian-export    Export memories to an Obsidian-compatible markdown vault
                       --vault         Path to Obsidian vault root (required)
                       --project       Filter export to a single project (optional)
                       --limit         Cap exported observations at N (optional)
                       --since         Export only observations after this date, e.g. 2026-01-01 (optional)
                       --force         Ignore incremental state, full re-export (optional)
                       --graph-config  Graph layout mode: preserve|force|skip (default: preserve)
                       --watch         Enable auto-sync mode (runs on interval until Ctrl+C)
                       --interval      Sync interval for --watch mode (default: 10m, minimum: 1m)

  version            Print version
  help               Show this help

Environment:
  ENGRAM_DATA_DIR    Override data directory (default: ~/.engram)
  ENGRAM_PORT        Override HTTP server port (default: 7437)
  ENGRAM_HOST        Bind interface for "engram serve" (default: 127.0.0.1).
                     Set it to 0.0.0.0 only behind a boundary that already
                     restricts who reaches the port, such as a container
                     publishing it on the host's loopback address.
  ENGRAM_PROJECT     Process-level default project override.
                     For "engram serve": fallback for GET /sync/status with no project param.
                     For "engram mcp": sets DefaultProject, overriding cwd detection for all tools.
  ENGRAM_TUI_THEME   Palette for "engram tui": koi-pond (default) | koi-day | showa | ogon |
                     catppuccin-mocha | kanagawa | elephant.
                     Precedence: --theme flag, then this var, then tui.theme in
                     <data-dir>/config.json, then the default.
  ENGRAM_HTTP_TOKEN  Optional Bearer auth for local HTTP server (engram serve).
                     When set, the following routes require Authorization: Bearer <token>:
                       DELETE /sessions/{id}, DELETE /observations/{id}, DELETE /prompts/{id},
                       GET /export, POST /import, POST /projects/migrate
                     Comparison is constant-time. Token is read per-request (no restart needed).
                     When unset, all routes are open (zero-config default).
  ENGRAM_TIMEZONE    Timezone for timestamp display in TUI and cloud dashboard.
                     Accepts any IANA zone name (e.g. America/New_York, Europe/Berlin).
                     Falls back to system local time when unset or invalid.
  ENGRAM_AGENT_CLI   LLM runner for conflicts scan --semantic (claude or opencode)
  ENGRAM_CLOUD_AUTOSYNC
                     Set to 1 to enable background autosync; also requires
                     ENGRAM_CLOUD_TOKEN and ENGRAM_CLOUD_SERVER
  ENGRAM_CLOUD_SERVER
                     Cloud server URL used by autosync and engram sync --cloud
  ENGRAM_PROJECTS_SYNC
                     Set to 1 to replicate the engram-projects rows (project cards,
                     tasks, evidence, task links, observation refs). Default 0:
                     leave it off until the cloud image and every teammate's binary
                     understand the new entities, otherwise their pull stops.
                     Applying what arrives is never gated by this flag.
  ENGRAM_DATABASE_URL
                     Postgres DSN for engram cloud serve
  ENGRAM_CLOUD_HOST  Bind host for engram cloud serve (default: 127.0.0.1)
  ENGRAM_CLOUD_MAX_PUSH_BYTES
                     Max cloud push payload bytes (default: 8388608)
  ENGRAM_CLOUD_TOKEN Bearer token required in authenticated cloud serve mode
  ENGRAM_CLOUD_INSECURE_NO_AUTH
                     Set to 1 ONLY for local insecure cloud serve mode (no auth)
                     Cannot be combined with ENGRAM_CLOUD_TOKEN
                     Cannot be combined with ENGRAM_CLOUD_ADMIN
  ENGRAM_CLOUD_ALLOWED_PROJECTS
                     Comma-separated project allowlist enforced by cloud server.
                     Required for cloud serve in BOTH token auth and insecure no-auth mode.
                     Use * to allow all projects (dev/internal deploys).
  ENGRAM_JWT_SECRET  Required in authenticated cloud serve mode (ENGRAM_CLOUD_TOKEN set);
                     must be explicitly set to a non-default value
  ENGRAM_CLOUD_ADMIN Optional admin-only dashboard token in authenticated mode
                     Ignored/rejected in insecure mode (ENGRAM_CLOUD_INSECURE_NO_AUTH=1)

MCP Configuration (add to your agent's config):
  {
    "mcp": {
      "engram": {
        "type": "stdio",
        "command": "engram",
        "args": ["mcp", "--tools=agent"]
      }
    }
  }
`, version)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "engram: %s\n", err)
	exitFunc(1)
}

// resolveHomeFallback tries platform-specific environment variables to find
// a home directory when os.UserHomeDir() fails. This commonly happens on
// Windows when engram is launched as an MCP subprocess without full env
// propagation.
func resolveHomeFallback() string {
	// Windows: try common env vars that might be set even when
	// %USERPROFILE% is missing.
	for _, env := range []string{"USERPROFILE", "HOME", "LOCALAPPDATA"} {
		if v := os.Getenv(env); v != "" {
			if env == "LOCALAPPDATA" {
				// LOCALAPPDATA is C:\Users\<user>\AppData\Local — go up two levels.
				parent := filepath.Dir(filepath.Dir(v))
				if parent != "." && parent != v {
					return parent
				}
			}
			return v
		}
	}

	// Unix: $HOME should always work, but try passwd-style fallback.
	if v := os.Getenv("HOME"); v != "" {
		return v
	}

	return ""
}

// migrateOrphanedDB checks for engram databases that ended up in wrong
// locations (e.g. drive root on Windows when UserHomeDir failed silently)
// and moves them to the correct location if the correct location has no DB.
func migrateOrphanedDB(correctDir string) {
	correctDB := filepath.Join(correctDir, "engram.db")

	// If the correct DB already exists, nothing to migrate.
	if _, err := os.Stat(correctDB); err == nil {
		return
	}

	// Known wrong locations: relative ".engram" resolved from common roots.
	// On Windows this typically ends up at C:\.engram or D:\.engram.
	candidates := []string{
		filepath.Join(string(filepath.Separator), ".engram", "engram.db"),
	}

	// On Windows, check all drive letter roots.
	if filepath.Separator == '\\' {
		for _, drive := range "CDEFGHIJ" {
			candidates = append(candidates,
				filepath.Join(string(drive)+":\\", ".engram", "engram.db"),
			)
		}
	}

	for _, candidate := range candidates {
		if candidate == correctDB {
			continue
		}
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}

		// Found an orphaned DB — migrate it.
		log.Printf("[engram] found orphaned database at %s, migrating to %s", candidate, correctDB)

		if err := os.MkdirAll(correctDir, 0755); err != nil {
			log.Printf("[engram] migration failed (create dir): %v", err)
			return
		}

		// Move DB and WAL/SHM files if they exist.
		for _, suffix := range []string{"", "-wal", "-shm"} {
			src := candidate + suffix
			dst := correctDB + suffix
			if _, statErr := os.Stat(src); statErr != nil {
				continue
			}
			if renameErr := os.Rename(src, dst); renameErr != nil {
				log.Printf("[engram] migration failed (move %s): %v", filepath.Base(src), renameErr)
				return
			}
		}

		// Clean up empty orphaned directory.
		orphanDir := filepath.Dir(candidate)
		entries, _ := os.ReadDir(orphanDir)
		if len(entries) == 0 {
			os.Remove(orphanDir)
		}

		log.Printf("[engram] migration complete — memories recovered")
		return
	}
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}
