package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/kdubb-labs/repo-sync-cli/internal/syncer"
)

type cliRunner struct {
	name string
	args []string
	body string
}

func (r *cliRunner) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	r.name = name
	r.args = args
	return r.body, nil
}

func TestRunRejectsInvalidBranchWithUsageExit(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"sync", "--root", t.TempDir(), "--org", "onyxpie", "--branch=--force"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exit code = %d, want 2; stderr = %s", exitCode, stderr.String())
	}
}

func TestExitCodeForResultReturnsUpstreamFailure(t *testing.T) {
	if got := exitCodeForResult(compactResult{Failed: 1}); got != 5 {
		t.Fatalf("exit code = %d, want 5", got)
	}
	if got := exitCodeForResult(compactResult{}); got != 0 {
		t.Fatalf("exit code = %d, want 0", got)
	}
}

func TestListOrganizationReposParsesGitHubResponse(t *testing.T) {
	runner := &cliRunner{body: `[
		[{"name":"clickops","ssh_url":"git@github.com:onyxpie/clickops.git","archived":false}],
		[{"name":"archive","ssh_url":"git@github.com:onyxpie/archive.git","archived":true}]
	]`}

	repos, err := listOrganizationRepos(context.Background(), runner, "onyxpie")
	if err != nil {
		t.Fatalf("listOrganizationRepos() error = %v", err)
	}
	if len(repos) != 2 || repos[0].Name != "clickops" || !repos[1].Archived {
		t.Fatalf("repos = %#v", repos)
	}
	if runner.name != "gh" {
		t.Fatalf("command = %q, want gh", runner.name)
	}
	if len(runner.args) != 4 || runner.args[0] != "api" || runner.args[1] != "--paginate" || runner.args[2] != "--slurp" {
		t.Fatalf("arguments = %#v, want gh api --paginate --slurp ENDPOINT", runner.args)
	}
}

var _ syncer.CommandRunner = (*cliRunner)(nil)
