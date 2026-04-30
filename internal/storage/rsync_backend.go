// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package storage

import (
	"context"
	"io"

	"github.com/jc-lab/backupgate/internal/config"
	"github.com/jc-lab/backupgate/internal/reader"
)

type rsyncUploadBackend struct {
	primary  Backend
	uploader *RsyncUploader
}

var _ Backend = (*rsyncUploadBackend)(nil)

// NewRsyncUploadBackend routes Upload/ResumeUpload through rsync and delegates
// every other storage operation to SFTP.
func NewRsyncUploadBackend(primary *SFTPBackend, cfg *config.RsyncConfig) Backend {
	return &rsyncUploadBackend{
		primary:  primary,
		uploader: NewRsyncUploader(cfg, primary.sshClient),
	}
}

func (b *rsyncUploadBackend) Upload(ctx context.Context, remotePath string, r reader.UploadReader, size int64) error {
	return b.uploader.Upload(ctx, remotePath, r, size)
}

func (b *rsyncUploadBackend) ResumeUpload(ctx context.Context, remotePath string, r reader.UploadReader, offset int64, totalSize int64) error {
	return b.uploader.ResumeUpload(ctx, remotePath, r, offset, totalSize)
}

func (b *rsyncUploadBackend) Download(ctx context.Context, remotePath string) (io.ReadCloser, error) {
	return b.primary.Download(ctx, remotePath)
}

func (b *rsyncUploadBackend) Delete(ctx context.Context, remotePath string) error {
	return b.primary.Delete(ctx, remotePath)
}

func (b *rsyncUploadBackend) List(ctx context.Context, remoteDir string) ([]Entry, error) {
	return b.primary.List(ctx, remoteDir)
}

func (b *rsyncUploadBackend) RemoteSize(ctx context.Context, remotePath string) (int64, error) {
	return b.primary.RemoteSize(ctx, remotePath)
}
