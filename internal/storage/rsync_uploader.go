// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"

	"github.com/gokrazy/rsync/rsyncclient"
	"github.com/jc-lab/backupgate/internal/config"
	"github.com/jc-lab/backupgate/internal/reader"
	"golang.org/x/crypto/ssh"
)

// RsyncUploader transfers upload payloads using the rsync protocol over the
// existing SSH connection. Other storage operations are intentionally left to
// the primary SFTP backend.
type RsyncUploader struct {
	basePath  string
	sshClient *ssh.Client
}

var _ Uploader = (*RsyncUploader)(nil)

// NewRsyncUploader creates an upload-only rsync adapter.
func NewRsyncUploader(cfg *config.RsyncConfig, sshClient *ssh.Client) *RsyncUploader {
	return &RsyncUploader{
		basePath:  cfg.BasePath,
		sshClient: sshClient,
	}
}

// Upload transfers the file using rsync's sender mode.
func (b *RsyncUploader) Upload(ctx context.Context, remotePath string, r reader.UploadReader, size int64) error {
	localPath, ok := r.FileName()
	if !ok {
		return fmt.Errorf("rsync upload requires file-backed upload reader")
	}
	if syncer, ok := r.(interface{ Sync() error }); ok {
		if err := syncer.Sync(); err != nil {
			return fmt.Errorf("syncing upload buffer: %w", err)
		}
	}

	return b.runRsyncOverSSH(ctx, b.remotePath(remotePath), []string{localPath})
}

// ResumeUpload uses rsync's delta protocol rather than an explicit byte offset.
func (b *RsyncUploader) ResumeUpload(ctx context.Context, remotePath string, r reader.UploadReader, offset int64, totalSize int64) error {
	return b.Upload(ctx, remotePath, r, totalSize)
}

func (b *RsyncUploader) runRsyncOverSSH(ctx context.Context, remotePath string, paths []string) error {
	var stderr bytes.Buffer

	client, err := rsyncclient.New([]string{}, rsyncclient.WithSender(), rsyncclient.WithStderr(&stderr))
	if err != nil {
		return fmt.Errorf("creating rsync client: %w", err)
	}

	err = b.rsyncDo(ctx, &stderr, client.ServerCommandOptions(remotePath), func(c *sshReadWriter) error {
		result, runErr := client.Run(ctx, c, paths)
		if result != nil && result.Stats != nil {
			slog.Debug("rsync upload complete", "stats", fmt.Sprintf("%+v", result.Stats))
		}
		return runErr
	})
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("rsync over SSH failed: %w: %s", err, msg)
		}
		return fmt.Errorf("rsync over SSH failed: %w", err)
	}
	return nil
}

func (b *RsyncUploader) remotePath(rel string) string {
	return path.Join(b.basePath, rel)
}

func (b *RsyncUploader) rsyncDo(ctx context.Context, stderr io.Writer, args []string, f func(c *sshReadWriter) error) error {
	session, err := b.sshClient.NewSession()
	if err != nil {
		return fmt.Errorf("creating SSH session: %w", err)
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("opening SSH stdin: %w", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("opening SSH stdout: %w", err)
	}
	session.Stderr = stderr

	command := remoteRsyncCommand(args)
	slog.Debug("running remote rsync", "command", command)
	if err := session.Start(command); err != nil {
		return fmt.Errorf("starting remote rsync: %w", err)
	}

	c := &sshReadWriter{Reader: stdout, Writer: stdin}

	done := make(chan error, 1)
	go func() {
		runErr := f(c)
		closeErr := stdin.Close()
		waitErr := session.Wait()
		switch {
		case runErr != nil:
			done <- fmt.Errorf("run rsync: %w", runErr)
		case closeErr != nil:
			done <- fmt.Errorf("closing SSH stdin: %w", closeErr)
		case waitErr != nil:
			done <- fmt.Errorf("waiting for SSH session: %w", waitErr)
		default:
			done <- nil
		}
	}()

	select {
	case <-ctx.Done():
		session.Close()
		return ctx.Err()
	case err := <-done:
		return err
	}
}

type sshReadWriter struct {
	io.Reader
	io.Writer
}

func remoteRsyncCommand(args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, "rsync")
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
