package lab

import "context"

// Public accessors used by PLAN01 (and later ACTs). These wrap the
// internal lowercase helpers so external packages can drive the lab
// without depending on private symbols.

// RunJJ invokes `jj` under the lab's HOME in the jj client workspace.
func (l *Lab) RunJJ(ctx context.Context, args []string) error {
	return l.runJJ(ctx, l.JJClient, args)
}

// RunGit invokes `git` under the lab's HOME in the given directory.
// If dir is empty, the lab root is used.
func (l *Lab) RunGit(ctx context.Context, dir string, args []string) error {
	return l.runGit(ctx, dir, args)
}

// JJOutput invokes `jj` and returns trimmed stdout.
func (l *Lab) JJOutput(ctx context.Context, dir string, args []string) (string, error) {
	return l.jjOutput(ctx, dir, args)
}

// GitOutput invokes `git` and returns trimmed stdout.
func (l *Lab) GitOutput(ctx context.Context, dir string, args []string) (string, error) {
	return l.gitOutput(ctx, dir, args)
}
