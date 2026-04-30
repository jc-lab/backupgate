// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// HtpasswdProvider authenticates against an Apache htpasswd format file.
// Supports bcrypt, SHA1, and MD5 password hashes.
type HtpasswdProvider struct {
	path    string
	entries map[string]string // username -> hashed password
}

// NewHtpasswdProvider creates an HtpasswdProvider from the given htpasswd file.
func NewHtpasswdProvider(path string) (*HtpasswdProvider, error) {
	p := &HtpasswdProvider{
		path:    path,
		entries: make(map[string]string),
	}
	if err := p.load(); err != nil {
		return nil, err
	}
	return p, nil
}

// Authenticate checks the password against the htpasswd entry for the user.
func (p *HtpasswdProvider) Authenticate(_ context.Context, req *AuthRequest) (string, error) {
	hashed, ok := p.entries[req.Username]
	if !ok {
		return "", fmt.Errorf("%w: user not found", ErrAuthentication)
	}

	if !p.verifyPassword(req.Password, hashed) {
		return "", fmt.Errorf("%w: invalid password", ErrAuthentication)
	}

	return req.Username, nil
}

// load reads and parses the htpasswd file.
func (p *HtpasswdProvider) load() error {
	f, err := os.Open(p.path)
	if err != nil {
		return fmt.Errorf("opening htpasswd file: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		user, hash, ok := strings.Cut(line, ":")
		if !ok || user == "" || hash == "" {
			return fmt.Errorf("invalid htpasswd entry at line %d", lineNo)
		}
		p.entries[user] = hash
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading htpasswd file: %w", err)
	}
	return nil
}

// verifyPassword checks a plaintext password against a hashed password.
// Supports bcrypt ($2y$, $2a$), SHA1 ({SHA}), and MD5 ($apr1$) formats.
func (p *HtpasswdProvider) verifyPassword(plain, hashed string) bool {
	if strings.HasPrefix(hashed, "$2y$") || strings.HasPrefix(hashed, "$2a$") || strings.HasPrefix(hashed, "$2b$") {
		normalized := "$2a$" + strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(hashed, "$2y$"), "$2a$"), "$2b$")
		return bcrypt.CompareHashAndPassword([]byte(normalized), []byte(plain)) == nil
	}
	if strings.HasPrefix(hashed, "{SHA}") {
		sum := sha1.Sum([]byte(plain))
		return "{SHA}"+base64.StdEncoding.EncodeToString(sum[:]) == hashed
	}
	if strings.HasPrefix(hashed, "$apr1$") {
		return apr1Hash(plain, hashed) == hashed
	}
	return false
}

func apr1Hash(password, hashed string) string {
	parts := strings.Split(hashed, "$")
	if len(parts) < 4 {
		return ""
	}
	return md5Crypt(password, parts[2], parts[3], "$apr1$")
}

const apr1Magic = "$apr1$"
const itoa64 = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func md5Crypt(password, salt, _ string, magic string) string {
	if magic == "" {
		magic = apr1Magic
	}
	if idx := strings.IndexByte(salt, '$'); idx >= 0 {
		salt = salt[:idx]
	}
	if len(salt) > 8 {
		salt = salt[:8]
	}

	pw := []byte(password)
	saltBytes := []byte(salt)
	ctx := md5.New()
	ctx.Write(pw)
	ctx.Write([]byte(magic))
	ctx.Write(saltBytes)

	alt := md5.New()
	alt.Write(pw)
	alt.Write(saltBytes)
	alt.Write(pw)
	final := alt.Sum(nil)

	for i := len(pw); i > 0; i -= 16 {
		if i > 16 {
			ctx.Write(final)
		} else {
			ctx.Write(final[:i])
		}
	}
	for i := len(pw); i > 0; i >>= 1 {
		if i&1 == 1 {
			ctx.Write([]byte{0})
		} else {
			ctx.Write(pw[:1])
		}
	}
	final = ctx.Sum(nil)

	for i := 0; i < 1000; i++ {
		round := md5.New()
		if i&1 == 1 {
			round.Write(pw)
		} else {
			round.Write(final)
		}
		if i%3 != 0 {
			round.Write(saltBytes)
		}
		if i%7 != 0 {
			round.Write(pw)
		}
		if i&1 == 1 {
			round.Write(final)
		} else {
			round.Write(pw)
		}
		final = round.Sum(nil)
	}

	var b strings.Builder
	b.WriteString(magic)
	b.WriteString(salt)
	b.WriteByte('$')
	b.WriteString(to64(uint32(final[0])<<16|uint32(final[6])<<8|uint32(final[12]), 4))
	b.WriteString(to64(uint32(final[1])<<16|uint32(final[7])<<8|uint32(final[13]), 4))
	b.WriteString(to64(uint32(final[2])<<16|uint32(final[8])<<8|uint32(final[14]), 4))
	b.WriteString(to64(uint32(final[3])<<16|uint32(final[9])<<8|uint32(final[15]), 4))
	b.WriteString(to64(uint32(final[4])<<16|uint32(final[10])<<8|uint32(final[5]), 4))
	b.WriteString(to64(uint32(final[11]), 2))
	return b.String()
}

func to64(v uint32, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteByte(itoa64[v&0x3f])
		v >>= 6
	}
	return b.String()
}
