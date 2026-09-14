package store

import (
	"strings"
	"testing"
)

// TestResolveProjectCardInheritsJiraProjectNotGraph pins the line the
// inheritance stops at: an instance borrows the pointers its umbrella set, and
// never the graph the umbrella happens to have built, because a graph describes
// one checkout and nothing else.
func TestResolveProjectCardInheritsJiraProjectNotGraph(t *testing.T) {
	s := newTestStore(t)

	if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{
		Slug:             "nextcloud",
		RepoURL:          strPtr("git@example.com:acme/nextcloud.git"),
		DefaultBranch:    strPtr("main"),
		JiraProject:      strPtr("CDBS"),
		JiraComponent:    strPtr("storage"),
		KnowledgeHubPath: strPtr("Services/Nextcloud"),
		Owner:            strPtr("platform"),
		GraphPath:        strPtr("out/graph.json"),
	}); err != nil {
		t.Fatalf("UpsertProjectCard(parent): %v", err)
	}
	commit := strings.Repeat("b", 40)
	if err := s.StampProjectGraph("nextcloud", commit, "2026-09-14 09:00:00", nil); err != nil {
		t.Fatalf("StampProjectGraph: %v", err)
	}

	if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{
		Slug:          "nextcloud-00",
		DefaultBranch: strPtr("develop"),
	}); err != nil {
		t.Fatalf("UpsertProjectCard(child): %v", err)
	}
	if err := s.SetProjectParent("nextcloud-00", strPtr("nextcloud")); err != nil {
		t.Fatalf("SetProjectParent: %v", err)
	}

	resolved, err := s.ResolveProjectCard("nextcloud-00")
	if err != nil {
		t.Fatalf("ResolveProjectCard: %v", err)
	}

	if resolved.JiraProject != "CDBS" {
		t.Fatalf("jira_project %q, want CDBS", resolved.JiraProject)
	}
	if resolved.Inherited["jira_project"] != "nextcloud" {
		t.Fatalf("jira_project came from %q, want nextcloud", resolved.Inherited["jira_project"])
	}
	if resolved.RepoURL == nil || *resolved.RepoURL != "git@example.com:acme/nextcloud.git" {
		t.Fatalf("repo_url %v", resolved.RepoURL)
	}
	if resolved.KnowledgeHubPath == nil || *resolved.KnowledgeHubPath != "Services/Nextcloud" {
		t.Fatalf("knowledge_hub_path %v", resolved.KnowledgeHubPath)
	}
	if resolved.GraphPath != "out/graph.json" || resolved.Inherited["graph_path"] != "nextcloud" {
		t.Fatalf("graph_path %q from %q", resolved.GraphPath, resolved.Inherited["graph_path"])
	}
	if resolved.Owner == nil || *resolved.Owner != "platform" {
		t.Fatalf("owner %v", resolved.Owner)
	}
	if resolved.JiraComponent == nil || *resolved.JiraComponent != "storage" {
		t.Fatalf("jira_component %v", resolved.JiraComponent)
	}

	// A value the child set itself is never replaced.
	if resolved.DefaultBranch != "develop" {
		t.Fatalf("default_branch %q, want develop", resolved.DefaultBranch)
	}
	if _, inherited := resolved.Inherited["default_branch"]; inherited {
		t.Fatal("a branch the child chose must not be reported as inherited")
	}

	// The graph never crosses the boundary.
	if resolved.GraphCommit != nil {
		t.Fatalf("graph_commit %v must not be inherited", *resolved.GraphCommit)
	}
	if resolved.GraphBuiltAt != nil || resolved.GraphSummary != nil {
		t.Fatalf("graph facts leaked: %v %v", resolved.GraphBuiltAt, resolved.GraphSummary)
	}
	for _, field := range []string{"graph_commit", "graph_built_at", "graph_summary"} {
		if _, inherited := resolved.Inherited[field]; inherited {
			t.Fatalf("%s must never be reported as inherited", field)
		}
	}

	// A root card resolves to itself with nothing inherited.
	root, err := s.ResolveProjectCard("nextcloud")
	if err != nil {
		t.Fatalf("ResolveProjectCard(root): %v", err)
	}
	if len(root.Inherited) != 0 {
		t.Fatalf("a root inherits nothing, got %v", root.Inherited)
	}
}
