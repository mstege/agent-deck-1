package main

import (
	"flag"
	"io"
	"strings"
	"testing"
)

// guardFlagSet mirrors the identity-carrying flags `add` and `launch` share,
// plus one flag that legitimately carries dashes.
func guardFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("guard", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.String("worktree", "", "")
	fs.String("w", "", "")
	fs.String("title", "", "")
	fs.String("model", "", "")
	fs.String("group", "", "")
	fs.String("cmd", "", "")
	fs.String("extra-arg", "", "")
	fs.Bool("new-branch", false, "")
	fs.Bool("b", false, "")
	return fs
}

func TestRejectSwallowedFlagValues(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantErr  bool
		contains string
	}{
		{
			// The 2026-09-09 sequence, verbatim: each of these created a real
			// branch (feature/-b, feature/--branch, feature/--model) because
			// the prefix from [worktree].branch_prefix was applied on top.
			name:     "--worktree swallows a following short bool flag",
			args:     []string{"--worktree", "-b", "."},
			wantErr:  true,
			contains: "--worktree -b",
		},
		{
			name:     "--worktree swallows a following long flag",
			args:     []string{"--worktree", "--new-branch", "."},
			wantErr:  true,
			contains: "--worktree --new-branch",
		},
		{
			name:     "--worktree swallows a following value flag",
			args:     []string{"--worktree", "--model", "opus", "."},
			wantErr:  true,
			contains: "--worktree --model",
		},
		{
			name:    "-w swallows just as -w is just as short",
			args:    []string{"-w", "-b", "."},
			wantErr: true,
		},
		{
			name:    "--title swallows too",
			args:    []string{"--title", "--group", "work", "."},
			wantErr: true,
		},
		{
			name:    "a real branch name passes",
			args:    []string{"--worktree", "feature/zustellung", "-b", "."},
			wantErr: false,
		},
		{
			name:    "a title with an inner dash passes",
			args:    []string{"--title", "zh-probe", "."},
			wantErr: false,
		},
		{
			name:    "an unset flag cannot trip the guard",
			args:    []string{"."},
			wantErr: false,
		},
		{
			// --extra-arg exists to pass CLI tokens through to the inner agent,
			// so a leading dash there is the point, not a mistake.
			name:    "flags that legitimately carry dashes are not guarded",
			args:    []string{"--extra-arg", "--verbose", "."},
			wantErr: false,
		},
		{
			name:    "--cmd carries a whole command line and is not guarded",
			args:    []string{"--cmd", "--weird-tool", "."},
			wantErr: false,
		},
		{
			// The documented escape hatch has to actually work.
			name:    "--flag=-value is accepted as deliberate",
			args:    []string{"--title=-leading-dash", "."},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fs := guardFlagSet()
			if err := fs.Parse(normalizeArgs(fs, tc.args)); err != nil {
				t.Fatalf("parse: %v", err)
			}
			err := rejectSwallowedFlagValues(fs, tc.args)
			if tc.wantErr && err == nil {
				t.Fatal("expected the swallowed value to be rejected, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if tc.contains != "" && !strings.Contains(err.Error(), tc.contains) {
				t.Errorf("error should name %q, got: %v", tc.contains, err)
			}
		})
	}
}

// The message must say what to do, not just that something is wrong: the
// operator's next move is either to supply the missing value or to use the
// --flag=value form.
func TestSwallowedFlagErrorIsActionable(t *testing.T) {
	fs := guardFlagSet()
	if err := fs.Parse(normalizeArgs(fs, []string{"--worktree", "-b", "."})); err != nil {
		t.Fatalf("parse: %v", err)
	}
	err := rejectSwallowedFlagValues(fs, []string{"--worktree", "-b", "."})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"swallowed", "--flag=-value"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}
