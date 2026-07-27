package syncer

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// CommandRunner executes git and GitHub CLI commands. It is injectable so the
// synchronization policy can be tested without modifying real repositories.
type CommandRunner interface {
	Run(context.Context, string, string, ...string) (string, error)
}

// OSRunner executes commands without a shell, avoiding argument interpolation.
type OSRunner struct{}

func (OSRunner) Run(ctx context.Context, dir, name string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return string(output), nil
}

// RemoteRepo is the small subset of GitHub repository metadata required for cloning.
type RemoteRepo struct {
	Name     string `json:"name"`
	SSHURL   string `json:"ssh_url"`
	CloneURL string `json:"clone_url"`
	Archived bool   `json:"archived"`
}

// Config controls one synchronization run.
type Config struct {
	Root     string
	CloneDir string
	Org      string
	Branch   string
	DryRun   bool
}

// Item records one compact, machine-readable result.
type Item struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Report contains every non-trivial result from a synchronization run.
type Report struct {
	Items []Item `json:"items"`
}

func (r Report) Count(status string) int {
	count := 0
	for _, item := range r.Items {
		if item.Status == status {
			count++
		}
	}
	return count
}

// Synchronizer safely refreshes local worktrees and discovers organization repositories.
type Synchronizer struct {
	Runner  CommandRunner
	ListOrg func(context.Context, string) ([]RemoteRepo, error)
}

func (s Synchronizer) Sync(ctx context.Context, config Config) (Report, error) {
	if config.Root == "" || config.Org == "" || config.Branch == "" {
		return Report{}, fmt.Errorf("root, org, and branch are required")
	}
	if err := ValidateBranch(config.Branch); err != nil {
		return Report{}, err
	}
	if config.CloneDir == "" {
		config.CloneDir = filepath.Join(config.Root, config.Org)
	}
	if s.Runner == nil {
		s.Runner = OSRunner{}
	}
	if s.ListOrg == nil {
		return Report{}, fmt.Errorf("organization repository lister is required")
	}

	repos, err := findGitRepositories(config.Root)
	if err != nil {
		return Report{}, err
	}

	report := Report{}
	knownRemotes := make(map[string]struct{})
	for _, repo := range repos {
		remote, remoteErr := s.Runner.Run(ctx, repo, "git", "remote", "get-url", "origin")
		if remoteErr != nil {
			report.add(repo, "skipped_no_origin", "origin remote is unavailable")
			continue
		}
		knownRemotes[canonicalRemote(remote)] = struct{}{}
		s.syncRepository(ctx, &report, repo, config)
	}

	organizationRepos, err := s.ListOrg(ctx, config.Org)
	if err != nil {
		return report, fmt.Errorf("list %s repositories: %w", config.Org, err)
	}
	for _, remote := range organizationRepos {
		if remote.Archived {
			report.add(remote.Name, "skipped_archived", "organization repository is archived")
			continue
		}
		if _, exists := knownRemotes[canonicalRemote(remote.SSHURL)]; exists {
			continue
		}
		destination := filepath.Join(config.CloneDir, remote.Name)
		if _, statErr := os.Lstat(destination); statErr == nil {
			report.add(destination, "skipped_destination_exists", "destination already exists")
			continue
		} else if !os.IsNotExist(statErr) {
			report.add(destination, "failed_destination_check", statErr.Error())
			continue
		}
		cloneURL := remote.CloneURL
		if cloneURL == "" {
			cloneURL = remote.SSHURL
		}
		if cloneURL == "" {
			report.add(destination, "failed_clone", "repository has no clone URL")
			continue
		}
		if config.DryRun {
			report.add(destination, "planned_clone", cloneURL)
			continue
		}
		if err := os.MkdirAll(config.CloneDir, 0o755); err != nil {
			report.add(destination, "failed_clone_directory", err.Error())
			continue
		}
		if _, cloneErr := s.Runner.Run(ctx, "", "git", "clone", "--", cloneURL, destination); cloneErr != nil {
			report.add(destination, "failed_clone", cloneErr.Error())
			continue
		}
		report.add(destination, "cloned", cloneURL)
		s.syncRepository(ctx, &report, destination, config)
	}

	return report, nil
}

func (s Synchronizer) syncRepository(ctx context.Context, report *Report, repo string, config Config) {
	status, err := s.Runner.Run(ctx, repo, "git", "status", "--porcelain")
	if err != nil {
		report.add(repo, "failed_status", err.Error())
		return
	}
	if strings.TrimSpace(status) != "" {
		report.add(repo, "skipped_dirty", "worktree has uncommitted changes")
		return
	}
	if config.DryRun {
		remoteHead := "refs/heads/" + config.Branch
		if _, err := s.Runner.Run(ctx, repo, "git", "ls-remote", "--exit-code", "origin", remoteHead); err != nil {
			report.add(repo, "skipped_no_origin_branch", remoteHead+" does not exist")
			return
		}
		report.add(repo, "planned_update", "would fetch, switch to target branch, and pull --ff-only")
		return
	}
	if _, err := s.Runner.Run(ctx, repo, "git", "fetch", "--prune", "origin"); err != nil {
		report.add(repo, "failed_fetch", err.Error())
		return
	}
	remoteBranch := "refs/remotes/origin/" + config.Branch
	if _, err := s.Runner.Run(ctx, repo, "git", "show-ref", "--verify", "--", remoteBranch); err != nil {
		report.add(repo, "skipped_no_origin_branch", remoteBranch+" does not exist")
		return
	}
	currentBranch, err := s.Runner.Run(ctx, repo, "git", "branch", "--show-current")
	if err != nil {
		report.add(repo, "failed_branch_check", err.Error())
		return
	}
	if strings.TrimSpace(currentBranch) != config.Branch {
		localBranch := "refs/heads/" + config.Branch
		if _, err := s.Runner.Run(ctx, repo, "git", "show-ref", "--verify", "--", localBranch); err == nil {
			if _, err := s.Runner.Run(ctx, repo, "git", "switch", "--", config.Branch); err != nil {
				report.add(repo, "failed_switch", err.Error())
				return
			}
		} else if _, err := s.Runner.Run(ctx, repo, "git", "switch", "--track", "-c", config.Branch, "origin/"+config.Branch); err != nil {
			report.add(repo, "failed_create_tracking_branch", err.Error())
			return
		}
	}
	divergence, err := s.Runner.Run(ctx, repo, "git", "rev-list", "--left-right", "--count", "HEAD...origin/"+config.Branch)
	if err != nil {
		report.add(repo, "failed_divergence_check", err.Error())
		return
	}
	ahead, behind, err := parseDivergence(divergence)
	if err != nil {
		report.add(repo, "failed_divergence_check", err.Error())
		return
	}
	if ahead > 0 && behind > 0 {
		report.add(repo, "skipped_diverged", fmt.Sprintf("local_ahead=%d remote_ahead=%d", ahead, behind))
		return
	}
	if _, err := s.Runner.Run(ctx, repo, "git", "pull", "--ff-only", "--", "origin", config.Branch); err != nil {
		report.add(repo, "failed_pull", err.Error())
		return
	}
	report.add(repo, "updated", config.Branch)
}

func (r *Report) add(path, status, detail string) {
	r.Items = append(r.Items, Item{Path: path, Status: status, Detail: detail})
}

func findGitRepositories(root string) ([]string, error) {
	var repositories []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Name() == ".git" {
			repositories = append(repositories, filepath.Dir(path))
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules") {
			return filepath.SkipDir
		}
		return nil
	})
	return repositories, err
}

func parseDivergence(output string) (int, int, error) {
	parts := strings.Fields(output)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected git divergence output %q", strings.TrimSpace(output))
	}
	ahead, err := strconv.Atoi(parts[0])
	if err != nil || ahead < 0 {
		return 0, 0, fmt.Errorf("invalid local-ahead count %q", parts[0])
	}
	behind, err := strconv.Atoi(parts[1])
	if err != nil || behind < 0 {
		return 0, 0, fmt.Errorf("invalid remote-ahead count %q", parts[1])
	}
	return ahead, behind, nil
}

// ValidateBranch rejects names that Git would parse ambiguously or reject as a branch.
func ValidateBranch(branch string) error {
	if !validBranch(branch) {
		return fmt.Errorf("branch %q is not a safe Git branch name", branch)
	}
	return nil
}

func validBranch(branch string) bool {
	if branch == "" || branch == "@" || strings.HasPrefix(branch, "-") || strings.HasSuffix(branch, "/") {
		return false
	}
	if strings.Contains(branch, "..") || strings.Contains(branch, "@{") || strings.Contains(branch, "//") {
		return false
	}
	for _, character := range branch {
		if character <= ' ' || character == 0x7f || strings.ContainsRune("~^:?*[\\", character) {
			return false
		}
	}
	for _, component := range strings.Split(branch, "/") {
		if component == "" || strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".") || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	return true
}

func canonicalRemote(remote string) string {
	value := strings.TrimSuffix(strings.TrimSpace(remote), ".git")
	value = strings.TrimPrefix(value, "ssh://")
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	value = strings.TrimPrefix(value, "git@")
	value = strings.Replace(value, ":", "/", 1)
	return strings.ToLower(value)
}
