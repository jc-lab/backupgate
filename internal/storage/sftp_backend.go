// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/jc-lab/backupgate/internal/config"
	"github.com/jc-lab/backupgate/internal/reader"
	"github.com/pkg/sftp"
)

// SFTPBackend implements Backend using the SFTP protocol.
type SFTPBackend struct {
	cfg      *config.SFTPConfig
	basePath string
}

var _ Backend = (*SFTPBackend)(nil)

// NewSFTPBackend creates a new SFTP storage backend.
func NewSFTPBackend(cfg *config.SFTPConfig) (*SFTPBackend, error) {
	b := &SFTPBackend{
		cfg:      cfg,
		basePath: cfg.BasePath,
	}
	return b, nil
}

func (b *SFTPBackend) HealthCheck(ctx context.Context) error {
	_ = ctx
	sshClient, err := sshConnect(&sshConnectInfo{
		Host:     b.cfg.Host,
		Username: b.cfg.Username,
		Password: b.cfg.Password,
		KeyFile:  b.cfg.KeyFile,
	})
	if err != nil {
		return fmt.Errorf("connecting SSH: %w", err)
	}
	defer sshClient.Close()

	client, err := sftp.NewClient(sshClient)
	if err != nil {
		return fmt.Errorf("creating SFTP client: %w", err)
	}
	defer client.Close()
	return nil
}

// Upload writes the full content to the remote path via SFTP.
func (b *SFTPBackend) Upload(ctx context.Context, remotePath string, r reader.UploadReader, size int64) error {
	_ = size
	if err := seekStart(r); err != nil {
		return err
	}
	fullPath := b.remotePath(remotePath)
	return b.withClient(func(client *sftp.Client) error {
		if err := client.MkdirAll(path.Dir(fullPath)); err != nil {
			return fmt.Errorf("creating remote directory: %w", err)
		}
		f, err := client.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
		if err != nil {
			return fmt.Errorf("creating remote file: %w", err)
		}
		defer f.Close()
		return copyWithContext(ctx, f, r)
	})
}

// ResumeUpload checks the remote file size and resumes from that offset.
// Uses SFTP append mode to continue writing from the offset.
func (b *SFTPBackend) ResumeUpload(ctx context.Context, remotePath string, r reader.UploadReader, offset int64, totalSize int64) error {
	_ = totalSize
	if err := seekTo(r, offset); err != nil {
		return err
	}
	fullPath := b.remotePath(remotePath)
	return b.withClient(func(client *sftp.Client) error {
		if err := client.MkdirAll(path.Dir(fullPath)); err != nil {
			return fmt.Errorf("creating remote directory: %w", err)
		}
		f, err := client.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY)
		if err != nil {
			return fmt.Errorf("opening remote file for resume: %w", err)
		}
		defer f.Close()
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return fmt.Errorf("seeking remote file: %w", err)
		}
		return copyWithContext(ctx, f, r)
	})
}

// Download reads the remote file for verification.
func (b *SFTPBackend) Download(ctx context.Context, remotePath string) (io.ReadCloser, error) {
	var out io.ReadCloser
	err := b.withClient(func(client *sftp.Client) error {
		f, err := client.Open(b.remotePath(remotePath))
		if err != nil {
			return fmt.Errorf("opening remote file: %w", err)
		}
		out = f
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Delete removes a file from the remote path.
func (b *SFTPBackend) Delete(ctx context.Context, remotePath string) error {
	_ = ctx
	return b.withClient(func(client *sftp.Client) error {
		if err := client.Remove(b.remotePath(remotePath)); err != nil {
			return fmt.Errorf("removing remote file: %w", err)
		}
		return nil
	})
}

// List returns entries in the remote directory.
func (b *SFTPBackend) List(ctx context.Context, remoteDir string) ([]Entry, error) {
	_ = ctx
	var entries []Entry
	err := b.withClient(func(client *sftp.Client) error {
		infos, err := client.ReadDir(b.remotePath(remoteDir))
		if err != nil {
			return fmt.Errorf("reading remote directory: %w", err)
		}
		entries = make([]Entry, 0, len(infos))
		for _, info := range infos {
			entries = append(entries, Entry{
				Name:    info.Name(),
				Size:    info.Size(),
				ModTime: info.ModTime(),
				IsDir:   info.IsDir(),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// RemoteSize returns the size of the remote file.
func (b *SFTPBackend) RemoteSize(ctx context.Context, remotePath string) (int64, error) {
	_ = ctx
	var size int64
	err := b.withClient(func(client *sftp.Client) error {
		info, err := client.Stat(b.remotePath(remotePath))
		if err != nil {
			return fmt.Errorf("stat remote file: %w", err)
		}
		size = info.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return size, nil
}

// remotePath joins the base path with the relative path.
func (b *SFTPBackend) remotePath(rel string) string {
	return path.Join(b.basePath, rel)
}

func seekStart(r reader.UploadReader) error {
	return seekTo(r, 0)
}

func seekTo(r reader.UploadReader, offset int64) error {
	if seeker, ok := r.(io.Seeker); ok {
		_, err := seeker.Seek(offset, io.SeekStart)
		if err != nil {
			return fmt.Errorf("seeking reader: %w", err)
		}
		return nil
	}
	if offset == 0 {
		return nil
	}
	return fmt.Errorf("reader does not support resume seek")
}

func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) error {
	buf := make([]byte, 128*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			if _, err := dst.Write(buf[:n]); err != nil {
				return fmt.Errorf("writing remote file: %w", err)
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("reading upload data: %w", readErr)
		}
	}
}

func (b *SFTPBackend) withClient(fn func(client *sftp.Client) error) error {
	sshClient, err := sshConnect(&sshConnectInfo{
		Host:     b.cfg.Host,
		Username: b.cfg.Username,
		Password: b.cfg.Password,
		KeyFile:  b.cfg.KeyFile,
	})
	if err != nil {
		return fmt.Errorf("connecting SSH: %w", err)
	}
	defer sshClient.Close()

	client, err := sftp.NewClient(sshClient)
	if err != nil {
		return fmt.Errorf("creating SFTP client: %w", err)
	}
	defer client.Close()

	return fn(client)
}
