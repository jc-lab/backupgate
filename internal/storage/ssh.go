// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package storage

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

type sshConnectInfo struct {
	Host     string
	Username string
	Password string
	KeyFile  string
}

func sshConnect(info *sshConnectInfo) (*ssh.Client, error) {
	var auths []ssh.AuthMethod
	if info.KeyFile != "" {
		key, err := os.ReadFile(info.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("reading SSH key: %w", err)
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf("parsing SSH key: %w", err)
		}
		auths = append(auths, ssh.PublicKeys(signer))
	}
	if info.Password != "" {
		auths = append(auths, ssh.Password(info.Password))
	}
	if len(auths) == 0 {
		return nil, fmt.Errorf("sftp: key_file or password is required")
	}

	host := info.Host
	if !strings.Contains(host, ":") {
		host += ":22"
	}
	return ssh.Dial("tcp", host, &ssh.ClientConfig{
		User:            info.Username,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
}

func sshCommand(client *ssh.Client, cmd string) (string, string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("creating SSH session: %w", err)
	}
	defer session.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	session.Stdout = &stdout
	session.Stderr = &stderr

	if err = session.Start(cmd); err != nil {
		return "", "", fmt.Errorf("starting SSH command: %w", err)
	}
	if err = session.Wait(); err != nil {
		return "", "", fmt.Errorf("waiting for SSH command: %w", err)
	}

	return stdout.String(), stderr.String(), nil
}
