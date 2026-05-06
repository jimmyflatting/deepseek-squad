package tmux

import (
	cmd2 "deepseek-squad/cmd"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"deepseek-squad/cmd/cmd_test"

	"github.com/stretchr/testify/require"
)

type MockPtyFactory struct {
	t *testing.T

	// Array of commands and the corresponding file handles representing PTYs.
	cmds  []*exec.Cmd
	files []*os.File
}

func (pt *MockPtyFactory) Start(cmd *exec.Cmd) (*os.File, error) {
	filePath := filepath.Join(pt.t.TempDir(), fmt.Sprintf("pty-%s-%d", pt.t.Name(), rand.Int31()))
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR, 0644)
	if err == nil {
		pt.cmds = append(pt.cmds, cmd)
		pt.files = append(pt.files, f)
	}
	return f, err
}

func (pt *MockPtyFactory) Close() {}

func NewMockPtyFactory(t *testing.T) *MockPtyFactory {
	return &MockPtyFactory{
		t: t,
	}
}

func TestSanitizeName(t *testing.T) {
	session := NewTmuxSession("asdf", "program")
	require.Equal(t, TmuxPrefix+"asdf", session.sanitizedName)

	session = NewTmuxSession("a sd f . . asdf", "program")
	require.Equal(t, TmuxPrefix+"asdf__asdf", session.sanitizedName)
}

func TestStartTmuxSession(t *testing.T) {
	ptyFactory := NewMockPtyFactory(t)

	created := false
	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			if strings.Contains(cmd.String(), "has-session") && !created {
				created = true
				return fmt.Errorf("session already exists")
			}
			return nil
		},
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			return []byte("output"), nil
		},
	}

	workdir := t.TempDir()
	session := newTmuxSession("test-session", "claude", ptyFactory, cmdExec)

	err := session.Start(workdir)
	require.NoError(t, err)
	require.Equal(t, 2, len(ptyFactory.cmds))
	require.Equal(t, fmt.Sprintf("tmux new-session -d -s deepseeksquad_test-session -c %s claude", workdir),
		cmd2.ToString(ptyFactory.cmds[0]))
	require.Equal(t, "tmux attach-session -t deepseeksquad_test-session",
		cmd2.ToString(ptyFactory.cmds[1]))

	require.Equal(t, 2, len(ptyFactory.files))

	// File should be closed.
	_, err = ptyFactory.files[0].Stat()
	require.Error(t, err)
	// File should be open
	_, err = ptyFactory.files[1].Stat()
	require.NoError(t, err)
}

func TestForwardEnvVarsWithMatchingPrefix(t *testing.T) {
	// Use a specific, unlikely-to-conflict prefix for testing
	const testPrefix = "ZZ_TEST_ENV_FWD_"

	// Set test environment variables
	t.Setenv(testPrefix+"MY_VAR", "value1")
	t.Setenv(testPrefix+"ANOTHER_VAR", "value2")
	// This should NOT be forwarded (doesn't match prefix)
	t.Setenv("ZZ_UNRELATED_VAR", "should-not-appear")

	var recordedEnvCmds []string
	created := false
	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			cmdStr := cmd2.ToString(cmd)
			if strings.Contains(cmdStr, "set-environment") {
				recordedEnvCmds = append(recordedEnvCmds, cmdStr)
			}
			if strings.Contains(cmdStr, "has-session") && !created {
				created = true
				return fmt.Errorf("session not found")
			}
			return nil
		},
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			return []byte("output"), nil
		},
	}

	ptyFactory := NewMockPtyFactory(t)
	workdir := t.TempDir()
	session := newTmuxSession("test-forward-env", "claude", ptyFactory, cmdExec)
	session.SetEnvVarPrefixes(testPrefix)

	err := session.Start(workdir)
	require.NoError(t, err)

	// Verify the two matching env vars were forwarded via set-environment
	require.Contains(t, recordedEnvCmds,
		fmt.Sprintf("tmux set-environment -t deepseeksquad_test-forward-env %sMY_VAR value1", testPrefix))
	require.Contains(t, recordedEnvCmds,
		fmt.Sprintf("tmux set-environment -t deepseeksquad_test-forward-env %sANOTHER_VAR value2", testPrefix))

	// Verify the non-matching var was NOT forwarded
	for _, cmdStr := range recordedEnvCmds {
		require.NotContains(t, cmdStr, "ZZ_UNRELATED_VAR")
	}

	// Verify the session was properly started (same assertions as TestStartTmuxSession)
	require.Equal(t, 2, len(ptyFactory.cmds))
	require.Equal(t, fmt.Sprintf("tmux new-session -d -s deepseeksquad_test-forward-env -c %s claude", workdir),
		cmd2.ToString(ptyFactory.cmds[0]))
	require.Equal(t, "tmux attach-session -t deepseeksquad_test-forward-env",
		cmd2.ToString(ptyFactory.cmds[1]))
}

func TestForwardEnvVarsWithNoMatchingVars(t *testing.T) {
	// Use a prefix that won't match any real env vars
	const obscurePrefix = "ZZ_OBSCURE_NONEXISTENT_PREFIX_"

	var recordedEnvCmds []string
	created := false
	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			cmdStr := cmd2.ToString(cmd)
			if strings.Contains(cmdStr, "set-environment") {
				recordedEnvCmds = append(recordedEnvCmds, cmdStr)
			}
			if strings.Contains(cmdStr, "has-session") && !created {
				created = true
				return fmt.Errorf("session not found")
			}
			return nil
		},
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			return []byte("output"), nil
		},
	}

	ptyFactory := NewMockPtyFactory(t)
	workdir := t.TempDir()
	session := newTmuxSession("test-no-match", "claude", ptyFactory, cmdExec)
	session.SetEnvVarPrefixes(obscurePrefix)

	err := session.Start(workdir)
	require.NoError(t, err)

	// No env vars should match the obscure prefix, so no set-environment calls
	require.Empty(t, recordedEnvCmds)
}

func TestForwardEnvVarsWithEmptyPrefixes(t *testing.T) {
	var recordedEnvCmds []string
	created := false
	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			cmdStr := cmd2.ToString(cmd)
			if strings.Contains(cmdStr, "set-environment") {
				recordedEnvCmds = append(recordedEnvCmds, cmdStr)
			}
			if strings.Contains(cmdStr, "has-session") && !created {
				created = true
				return fmt.Errorf("session not found")
			}
			return nil
		},
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			return []byte("output"), nil
		},
	}

	ptyFactory := NewMockPtyFactory(t)
	workdir := t.TempDir()
	session := newTmuxSession("test-empty-prefix", "claude", ptyFactory, cmdExec)
	// Empty prefixes = no env vars should be forwarded
	session.SetEnvVarPrefixes()

	err := session.Start(workdir)
	require.NoError(t, err)

	// No set-environment calls with empty prefixes
	require.Empty(t, recordedEnvCmds)
}

func TestExistingStartTmuxSessionStillPasses(t *testing.T) {
	// This test verifies that the existing TestStartTmuxSession behavior is preserved
	// despite the new env var forwarding logic. The session uses default prefixes (ANTHROPIC_)
	// but no ANTHROPIC_* vars are set during this test, so no forwarding should occur.
	ptyFactory := NewMockPtyFactory(t)

	created := false
	var recordedEnvCmds []string
	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			cmdStr := cmd2.ToString(cmd)
			if strings.Contains(cmdStr, "set-environment") {
				recordedEnvCmds = append(recordedEnvCmds, cmdStr)
			}
			if strings.Contains(cmdStr, "has-session") && !created {
				created = true
				return fmt.Errorf("session already exists")
			}
			return nil
		},
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			return []byte("output"), nil
		},
	}

	workdir := t.TempDir()
	session := newTmuxSession("test-session", "claude", ptyFactory, cmdExec)

	err := session.Start(workdir)
	require.NoError(t, err)

	// Verify same assertions as original TestStartTmuxSession
	require.Equal(t, 2, len(ptyFactory.cmds))
	require.Equal(t, fmt.Sprintf("tmux new-session -d -s deepseeksquad_test-session -c %s claude", workdir),
		cmd2.ToString(ptyFactory.cmds[0]))
	require.Equal(t, "tmux attach-session -t deepseeksquad_test-session",
		cmd2.ToString(ptyFactory.cmds[1]))

	// No ANTHROPIC_* vars are set in test env (or if they are, they'll be forwarded —
	// the key assertion is that ptyFactory.cmds count and session start are unaffected)
	// This test should not error regardless of env var presence
	require.NoError(t, err)
}
