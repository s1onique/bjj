package lab

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/s1onique/bjj/internal/execx"
)

// labHome returns the lab scratch HOME directory (always non-empty
// after Setup). All subprocess invocations in the lab should set HOME
// to this value so the developer's home directory and credential helper
// are never touched.
func (l *Lab) labHome() string {
	if l == nil {
		return ""
	}
	return l.Home
}

// runGit invokes git under the lab's HOME. If dir is empty, a temp
// directory is allocated for cwd.
func (l *Lab) runGit(ctx context.Context, dir string, args []string) error {
	cwd, cleanup, err := l.resolveCwd(dir)
	if err != nil {
		return err
	}
	defer cleanup()
	return runGitIn(ctx, cwd, l.labHome(), args)
}

// runJJ invokes jj under the lab's HOME.
func (l *Lab) runJJ(ctx context.Context, dir string, args []string) error {
	cwd, cleanup, err := l.resolveCwd(dir)
	if err != nil {
		return err
	}
	defer cleanup()
	return runJJIn(ctx, cwd, l.labHome(), args)
}

// gitOutput returns git stdout under the lab's HOME.
func (l *Lab) gitOutput(ctx context.Context, dir string, args []string) (string, error) {
	cwd, cleanup, err := l.resolveCwd(dir)
	if err != nil {
		return "", err
	}
	defer cleanup()
	return gitOutputIn(ctx, cwd, l.labHome(), args)
}

// jjOutput returns jj stdout under the lab's HOME.
func (l *Lab) jjOutput(ctx context.Context, dir string, args []string) (string, error) {
	cwd, cleanup, err := l.resolveCwd(dir)
	if err != nil {
		return "", err
	}
	defer cleanup()
	return jjOutputIn(ctx, cwd, l.labHome(), args)
}

// resolveCwd normalises the cwd argument: empty -> fresh temp dir;
// otherwise the input. The returned cleanup removes any allocated
// temp directory.
func (l *Lab) resolveCwd(dir string) (string, func(), error) {
	if dir != "" {
		return dir, func() {}, nil
	}
	tmp, err := os.MkdirTemp("", "bjj-lab-cwd-")
	if err != nil {
		return "", nil, fmt.Errorf("mkdtemp cwd: %w", err)
	}
	return tmp, func() { os.RemoveAll(tmp) }, nil
}

// runGitIn is runGit with explicit HOME.
func runGitIn(ctx context.Context, cwd, home string, args []string) error {
	res := execx.Run(ctx, execx.Request{
		Program: "git",
		Args:    args,
		Dir:     cwd,
		Env:     gitEnv(cwd, home),
	})
	if res.Err != nil {
		return fmt.Errorf("%w (stderr: %s)", res.Err, strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

// runJJIn is runJJ with explicit HOME.
func runJJIn(ctx context.Context, cwd, home string, args []string) error {
	res := execx.Run(ctx, execx.Request{
		Program: "jj",
		Args:    args,
		Dir:     cwd,
		Env:     jjEnv(cwd, home),
	})
	if res.Err != nil {
		return fmt.Errorf("%w (stderr: %s)", res.Err, strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

// gitOutputIn is gitOutput with explicit HOME.
func gitOutputIn(ctx context.Context, cwd, home string, args []string) (string, error) {
	res := execx.Run(ctx, execx.Request{
		Program: "git",
		Args:    args,
		Dir:     cwd,
		Env:     gitEnv(cwd, home),
	})
	if res.Err != nil {
		return "", fmt.Errorf("%w (stderr: %s)", res.Err, strings.TrimSpace(string(res.Stderr)))
	}
	return strings.TrimRight(string(res.Stdout), "\n"), nil
}

// jjOutputIn is jjOutput with explicit HOME.
func jjOutputIn(ctx context.Context, cwd, home string, args []string) (string, error) {
	res := execx.Run(ctx, execx.Request{
		Program: "jj",
		Args:    args,
		Dir:     cwd,
		Env:     jjEnv(cwd, home),
	})
	if res.Err != nil {
		return "", fmt.Errorf("%w (stderr: %s)", res.Err, strings.TrimSpace(string(res.Stderr)))
	}
	return strings.TrimRight(string(res.Stdout), "\n"), nil
}

// gitEnv builds the env overlay for a git invocation. The lab HOME is
// always exported; if home is empty we fall back to a temp dir so that
// git/jj still have a writable HOME.
func gitEnv(cwd, home string) []string {
	h, xdg := homeDirs(home, cwd)
	return []string{
		"GIT_AUTHOR_NAME=bjj-lab",
		"GIT_AUTHOR_EMAIL=bjj-lab@example.invalid",
		"GIT_COMMITTER_NAME=bjj-lab",
		"GIT_COMMITTER_EMAIL=bjj-lab@example.invalid",
		"GIT_CONFIG_NOSYSTEM=1",
		"HOME=" + h,
		"XDG_CONFIG_HOME=" + xdg,
	}
}

// jjEnv builds the env overlay for a jj invocation.
func jjEnv(cwd, home string) []string {
	h, xdg := homeDirs(home, cwd)
	return []string{
		"JJ_CONFIG=" + filepath.Join(xdg, "jj-disabled-config.toml"),
		"HOME=" + h,
		"XDG_CONFIG_HOME=" + xdg,
	}
}

// homeDirs returns (home, xdgConfigHome). If home is empty, a temp
// directory is allocated for HOME and xdgConfigHome, but the caller is
// responsible for cleanup of that directory in that rare case.
func homeDirs(home, cwd string) (string, string) {
	if home == "" {
		tmp, err := os.MkdirTemp("", "bjj-lab-home-")
		if err != nil {
			// Last-ditch: return a value derived from cwd. The child
			// may fail to write but the test will still be diagnosable.
			return cwd, filepath.Join(cwd, ".xdg-config")
		}
		return tmp, filepath.Join(tmp, "xdg-config")
	}
	return home, filepath.Join(home, "xdg-config")
}

// configureTestIdentity sets a per-repository test identity so the lab
// never depends on the developer's global git user.name/user.email or
// any signing helper.
func (l *Lab) configureTestIdentity(ctx context.Context, repoDir string) error {
	for _, kv := range []string{
		"user.name=bjj-lab",
		"user.email=bjj-lab@example.invalid",
		"commit.gpgsign=false",
		"tag.gpgsign=false",
		"push.gpgsign=false",
	} {
		parts := strings.SplitN(kv, "=", 2)
		if err := l.runGit(ctx, repoDir, []string{"config", "--local", parts[0], parts[1]}); err != nil {
			return err
		}
	}
	return nil
}

// captureRealOriginFromProject reads the current project's origin URL.
// Returns "" if no origin is configured.
func captureRealOriginFromProject() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
	res := execx.Run(context.Background(), execx.Request{
		Program: "git",
		Args:    []string{"-C", dir, "remote", "get-url", "origin"},
		Dir:     dir,
	})
	if res.Err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// jjVersionOutput returns the canonical first line of `jj version`.
func jjVersionOutput(ctx context.Context) (string, error) {
	// Probe under a one-shot temp HOME so this works even outside a lab.
	cwd, err := os.MkdirTemp("", "bjj-lab-version-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(cwd)
	home, xdg := homeDirs("", cwd)
	_ = home
	res := execx.Run(ctx, execx.Request{
		Program: "jj",
		Args:    []string{"version"},
		Dir:     cwd,
		Env: []string{
			"JJ_CONFIG=" + filepath.Join(xdg, "jj-disabled-config.toml"),
			"XDG_CONFIG_HOME=" + xdg,
		},
	})
	if res.Err != nil {
		return "", fmt.Errorf("%w (stderr: %s)", res.Err, strings.TrimSpace(string(res.Stderr)))
	}
	return strings.TrimRight(string(res.Stdout), "\n"), nil
}

// gitVersionOutput returns the canonical first line of `git --version`.
func gitVersionOutput(ctx context.Context) (string, error) {
	cwd, err := os.MkdirTemp("", "bjj-lab-version-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(cwd)
	return gitOutputIn(ctx, cwd, "", []string{"--version"})
}
