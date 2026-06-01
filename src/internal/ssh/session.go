package ssh

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	gossh "golang.org/x/crypto/ssh"
)

// Session wraps an ssh.Session with context-aware execution and convenience
// methods. Every method that takes a context will cancel the remote command
// when the context expires.
type Session struct {
	sess *gossh.Session
}

// newSession wraps a raw ssh.Session.
func newSession(s *gossh.Session) *Session {
	return &Session{sess: s}
}

// Run executes cmd on the remote host and returns combined output (stdout +
// stderr joined). The command is cancelled if ctx expires.
func (s *Session) Run(ctx context.Context, cmd string) (string, error) {
	var buf bytes.Buffer
	s.sess.Stdout = &buf
	s.sess.Stderr = &buf

	if err := s.sess.Start(cmd); err != nil {
		return buf.String(), fmt.Errorf("ssh: start %q: %w", cmd, err)
	}

	done := make(chan error, 1)
	go func() { done <- s.sess.Wait() }()

	select {
	case <-ctx.Done():
		s.sess.Close()
		return buf.String(), ctx.Err()
	case err := <-done:
		return buf.String(), err
	}
}

// Output executes cmd on the remote host and returns stdout only. Stderr is
// captured separately and returned as stderr string even on success (some
// commands write informational messages to stderr).
func (s *Session) Output(ctx context.Context, cmd string) (stdout string, stderr string, err error) {
	var outBuf, errBuf bytes.Buffer
	s.sess.Stdout = &outBuf
	s.sess.Stderr = &errBuf

	if err := s.sess.Start(cmd); err != nil {
		return outBuf.String(), errBuf.String(), fmt.Errorf("ssh: start %q: %w", cmd, err)
	}

	done := make(chan error, 1)
	go func() { done <- s.sess.Wait() }()

	select {
	case <-ctx.Done():
		s.sess.Close()
		return outBuf.String(), errBuf.String(), ctx.Err()
	case err := <-done:
		return outBuf.String(), errBuf.String(), err
	}
}

// RunScript pipes a multi-line bash script to the remote host via bash -s.
func (s *Session) RunScript(ctx context.Context, script string) (stdout string, stderr string, err error) {
	var outBuf, errBuf bytes.Buffer
	s.sess.Stdout = &outBuf
	s.sess.Stderr = &errBuf
	s.sess.Stdin = strings.NewReader(script)

	if err := s.sess.Start("bash -s"); err != nil {
		return outBuf.String(), errBuf.String(), fmt.Errorf("ssh: start bash script: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- s.sess.Wait() }()

	select {
	case <-ctx.Done():
		s.sess.Close()
		return outBuf.String(), errBuf.String(), ctx.Err()
	case err := <-done:
		return outBuf.String(), errBuf.String(), err
	}
}

// Close releases the session. Always call this when done with the session,
// even after errors.
func (s *Session) Close() error {
	if s.sess == nil {
		return nil
	}
	return s.sess.Close()
}
