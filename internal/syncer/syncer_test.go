package syncer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testResult struct {
	output string
	err    error
}

type fakeRunner struct {
	calls   []string
	results map[string]testResult
}

func (r *fakeRunner) Run(_ context.Context, dir, name string, args ...string) (string, error) {
	call := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.calls = append(r.calls, filepath.Base(dir)+":"+call)
	result, ok := r.results[call]
	if !ok {
		return "", nil
	}
	return result.output, result.err
}

func TestSyncDryRunPlansMissingOrganizationCloneWithoutMutation(t *testing.T) {
	root := t.TempDir()
	runner := &fakeRunner{}
	s := Synchronizer{
		Runner: runner,
		ListOrg: func(context.Context, string) ([]RemoteRepo, error) {
			return []RemoteRepo{{Name: "new-repo", SSHURL: "git@github.com:onyxpie/new-repo.git", CloneURL: "https://github.com/onyxpie/new-repo.git"}}, nil
		},
	}

	report, err := s.Sync(context.Background(), Config{
		Root:     root,
		CloneDir: filepath.Join(root, "onyxpie"),
		Org:      "onyxpie",
		Branch:   "main",
		DryRun:   true,
	})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if report.Count("planned_clone") != 1 {
		t.Fatalf("planned clone count = %d, want 1; report = %#v", report.Count("planned_clone"), report.Items)
	}
	if report.Items[0].Detail != "https://github.com/onyxpie/new-repo.git" {
		t.Fatalf("planned clone URL = %q, want HTTPS clone URL", report.Items[0].Detail)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "git clone") {
			t.Fatalf("dry run executed clone: %s", call)
		}
	}
}

func TestSyncDoesNotPlanCloneForEquivalentHTTPSAndSSHRemote(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "existing")
	if err := makeGitDirectory(repo); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{results: map[string]testResult{
		"git status --porcelain":    {},
		"git remote get-url origin": {output: "https://github.com/onyxpie/existing.git\n"},
	}}
	s := Synchronizer{
		Runner: runner,
		ListOrg: func(context.Context, string) ([]RemoteRepo, error) {
			return []RemoteRepo{{Name: "existing", SSHURL: "git@github.com:onyxpie/existing.git"}}, nil
		},
	}

	report, err := s.Sync(context.Background(), Config{Root: root, CloneDir: filepath.Join(root, "onyxpie"), Org: "onyxpie", Branch: "main", DryRun: true})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if report.Count("planned_clone") != 0 {
		t.Fatalf("equivalent remote was planned for cloning: %#v", report.Items)
	}
}

func TestSyncRejectsDangerousBranchBeforeRunningCommands(t *testing.T) {
	runner := &fakeRunner{}
	s := Synchronizer{Runner: runner, ListOrg: func(context.Context, string) ([]RemoteRepo, error) { return nil, nil }}

	_, err := s.Sync(context.Background(), Config{Root: t.TempDir(), Org: "onyxpie", Branch: "--force"})
	if err == nil {
		t.Fatal("Sync() accepted a branch beginning with a dash")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("invalid branch executed commands: %#v", runner.calls)
	}
}

func TestSyncDryRunSkipsRepositoryWithoutOriginBranch(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "dev-only")
	if err := makeGitDirectory(repo); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{results: map[string]testResult{
		"git status --porcelain":                           {},
		"git remote get-url origin":                        {output: "git@github.com:onyxpie/dev-only.git\n"},
		"git ls-remote --exit-code origin refs/heads/main": {err: errors.New("not found")},
	}}
	s := Synchronizer{Runner: runner, ListOrg: func(context.Context, string) ([]RemoteRepo, error) { return nil, nil }}

	report, err := s.Sync(context.Background(), Config{Root: root, Org: "onyxpie", Branch: "main", DryRun: true})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if report.Count("skipped_no_origin_branch") != 1 || report.Count("planned_update") != 0 {
		t.Fatalf("dry-run report = %#v", report.Items)
	}
}

func TestFindGitRepositoriesIncludesLinkedWorktree(t *testing.T) {
	root := t.TempDir()
	worktree := filepath.Join(root, "linked-worktree")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: /tmp/common/.git/worktrees/linked\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	repositories, err := findGitRepositories(root)
	if err != nil {
		t.Fatalf("findGitRepositories() error = %v", err)
	}
	if len(repositories) != 1 || repositories[0] != worktree {
		t.Fatalf("repositories = %#v, want [%q]", repositories, worktree)
	}
}

func TestSyncClonesWithOptionTerminator(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	runner := &fakeRunner{}
	s := Synchronizer{
		Runner: runner,
		ListOrg: func(context.Context, string) ([]RemoteRepo, error) {
			return []RemoteRepo{{Name: "-new-repo", SSHURL: "git@github.com:onyxpie/new-repo.git", CloneURL: "https://github.com/onyxpie/new-repo.git"}}, nil
		},
	}

	_, err := s.Sync(context.Background(), Config{Root: root, CloneDir: "--clone-target", Org: "onyxpie", Branch: "main"})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	want := "git clone -- https://github.com/onyxpie/new-repo.git --clone-target/-new-repo"
	found := false
	for _, call := range runner.calls {
		if strings.Contains(call, want) {
			found = true
		}
	}
	if !found {
		t.Fatalf("clone call with option terminator not found: %#v", runner.calls)
	}
}

func TestSyncSkipsDirtyRepositoryWithoutFetchOrBranchChange(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "dirty")
	if err := makeGitDirectory(repo); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{results: map[string]testResult{
		"git status --porcelain":    {output: " M README.md\n"},
		"git remote get-url origin": {output: "git@github.com:owner/dirty.git\n"},
	}}
	s := Synchronizer{Runner: runner, ListOrg: func(context.Context, string) ([]RemoteRepo, error) { return nil, nil }}

	report, err := s.Sync(context.Background(), Config{Root: root, CloneDir: filepath.Join(root, "onyxpie"), Org: "onyxpie", Branch: "main"})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if report.Count("skipped_dirty") != 1 {
		t.Fatalf("dirty skip count = %d, want 1; report = %#v", report.Count("skipped_dirty"), report.Items)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "fetch") || strings.Contains(call, "switch") || strings.Contains(call, "pull") {
			t.Fatalf("dirty repository was mutated: %s", call)
		}
	}
}

func TestSyncSkipsRepositoryWithoutOriginMain(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "dev-only")
	if err := makeGitDirectory(repo); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{results: map[string]testResult{
		"git status --porcelain":                            {},
		"git remote get-url origin":                         {output: "git@github.com:onyxpie/dev-only.git\n"},
		"git fetch --prune origin":                          {},
		"git show-ref --verify -- refs/remotes/origin/main": {err: errors.New("not found")},
	}}
	s := Synchronizer{Runner: runner, ListOrg: func(context.Context, string) ([]RemoteRepo, error) { return nil, nil }}

	report, err := s.Sync(context.Background(), Config{Root: root, CloneDir: filepath.Join(root, "onyxpie"), Org: "onyxpie", Branch: "main"})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if report.Count("skipped_no_origin_branch") != 1 {
		t.Fatalf("no-main skip count = %d, want 1; report = %#v", report.Count("skipped_no_origin_branch"), report.Items)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "switch") || strings.Contains(call, "pull") {
			t.Fatalf("repository without origin/main was mutated: %s", call)
		}
	}
}
