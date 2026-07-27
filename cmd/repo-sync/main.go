package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kdubb-labs/repo-sync-cli/internal/syncer"
)

const version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "agent-context" {
		_ = json.NewEncoder(stdout).Encode(map[string]any{
			"version":    version,
			"commands":   []string{"sync", "agent-context"},
			"sync_flags": []string{"--root", "--clone-dir", "--org", "--branch", "--dry-run", "--json"},
			"safety":     []string{"skips dirty worktrees", "uses pull --ff-only", "never resets or force-checks out", "skips repos without origin/<branch>"},
		})
		return 0
	}
	if len(args) > 0 && args[0] == "sync" {
		args = args[1:]
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "error: determine home directory: %v\n", err)
		return 1
	}
	rootDefault := filepath.Join(home, "git")
	flags := flag.NewFlagSet("repo-sync", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", rootDefault, "directory containing local repositories")
	cloneDir := flags.String("clone-dir", "", "directory for newly discovered organization repositories")
	org := flags.String("org", "onyxpie", "GitHub organization to discover")
	branch := flags.String("branch", "main", "required remote branch")
	dryRun := flags.Bool("dry-run", false, "report operations without fetching, switching, pulling, or cloning")
	jsonOutput := flags.Bool("json", false, "emit one compact JSON report")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: repo-sync [sync] [--root DIR] [--clone-dir DIR] [--org ORG] [--branch BRANCH] [--dry-run] [--json]")
		fmt.Fprintln(stderr, "Safely updates clean repositories with git pull --ff-only and clones missing non-archived organization repositories.")
	}
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || strings.TrimSpace(*org) == "" || strings.TrimSpace(*branch) == "" {
		flags.Usage()
		return 2
	}
	if err := syncer.ValidateBranch(*branch); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	if *cloneDir == "" {
		*cloneDir = filepath.Join(*root, *org)
	}

	runner := syncer.OSRunner{}
	synchronizer := syncer.Synchronizer{
		Runner: runner,
		ListOrg: func(ctx context.Context, organization string) ([]syncer.RemoteRepo, error) {
			return listOrganizationRepos(ctx, runner, organization)
		},
	}
	report, err := synchronizer.Sync(context.Background(), syncer.Config{
		Root: *root, CloneDir: *cloneDir, Org: *org, Branch: *branch, DryRun: *dryRun,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 5
	}
	result := compactReport(report, *dryRun)
	if *jsonOutput {
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			fmt.Fprintf(stderr, "error: write JSON report: %v\n", err)
			return 1
		}
		return exitCodeForResult(result)
	}
	fmt.Fprintf(stdout, "updated=%d cloned=%d planned_update=%d planned_clone=%d skipped=%d failed=%d\n",
		result.Counts["updated"], result.Counts["cloned"], result.Counts["planned_update"], result.Counts["planned_clone"], result.Skipped, result.Failed)
	for _, item := range result.Issues {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", item.Status, item.Path, item.Detail)
	}
	return exitCodeForResult(result)
}

type compactResult struct {
	DryRun  bool           `json:"dry_run"`
	Counts  map[string]int `json:"counts"`
	Skipped int            `json:"skipped"`
	Failed  int            `json:"failed"`
	Issues  []syncer.Item  `json:"issues,omitempty"`
}

func compactReport(report syncer.Report, dryRun bool) compactResult {
	result := compactResult{DryRun: dryRun, Counts: map[string]int{}}
	for _, item := range report.Items {
		result.Counts[item.Status]++
		if strings.HasPrefix(item.Status, "skipped_") {
			result.Skipped++
			result.Issues = append(result.Issues, item)
		}
		if strings.HasPrefix(item.Status, "failed_") {
			result.Failed++
			result.Issues = append(result.Issues, item)
		}
	}
	sort.Slice(result.Issues, func(i, j int) bool {
		if result.Issues[i].Status == result.Issues[j].Status {
			return result.Issues[i].Path < result.Issues[j].Path
		}
		return result.Issues[i].Status < result.Issues[j].Status
	})
	return result
}

func exitCodeForResult(result compactResult) int {
	if result.Failed > 0 {
		return 5
	}
	return 0
}

func listOrganizationRepos(ctx context.Context, runner syncer.CommandRunner, organization string) ([]syncer.RemoteRepo, error) {
	endpoint := fmt.Sprintf("/orgs/%s/repos?type=all&per_page=100", url.PathEscape(organization))
	output, err := runner.Run(ctx, "", "gh", "api", "--paginate", "--slurp", endpoint)
	if err != nil {
		return nil, err
	}
	var pages [][]syncer.RemoteRepo
	if err := json.Unmarshal([]byte(output), &pages); err == nil {
		var repos []syncer.RemoteRepo
		for _, page := range pages {
			repos = append(repos, page...)
		}
		return repos, nil
	}
	var repos []syncer.RemoteRepo
	if err := json.Unmarshal([]byte(output), &repos); err != nil {
		return nil, fmt.Errorf("parse GitHub repository response: %w", err)
	}
	return repos, nil
}
