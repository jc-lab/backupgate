// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/jc-lab/backupgate/internal/auth"
	"github.com/jc-lab/backupgate/internal/config"
	"github.com/jc-lab/backupgate/internal/requestmeta"
)

type AwsRequest struct {
	*http.Request
	body *hashedReader

	RequestId string

	Key    string
	KeyCfg *config.KeyConfig

	ExpectedContentSha256 string
	AuthorizedHeaders     http.Header
}

func NewAwsRequest(r *http.Request) *AwsRequest {
	areq := &AwsRequest{
		Request: r,
		body:    newHashedReader(r.Body, sha256.New()),
		Key:     extractKey(r),
	}

	if info := requestmeta.FromContext(r.Context()); info != nil {
		areq.RequestId = info.RequestID
	} else {
		areq.RequestId = newRequestID()
	}

	return areq
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (areq *AwsRequest) Authenticate(chain *auth.Chain) (string, error) {
	authz := areq.Request.Header.Get("Authorization")
	if !strings.HasPrefix(authz, "AWS4-HMAC-SHA256 ") {
		return "", fmt.Errorf("missing AWS4-HMAC-SHA256 authorization")
	}

	fields := parseAuthFields(strings.TrimPrefix(authz, "AWS4-HMAC-SHA256 "))
	credential := strings.Split(fields["Credential"], "/")
	if len(credential) != 5 {
		return "", fmt.Errorf("invalid credential scope")
	}
	accessKey := credential[0]
	date, region, service := credential[1], credential[2], credential[3]
	if service != "s3" || credential[4] != "aws4_request" {
		return accessKey, fmt.Errorf("unsupported credential scope")
	}

	secret, ok := chain.LookupSecret(accessKey)
	if !ok {
		return accessKey, fmt.Errorf("unknown access key")
	}

	signedHeaders := fields["SignedHeaders"]
	providedSignature := fields["Signature"]
	if signedHeaders == "" || providedSignature == "" {
		return accessKey, fmt.Errorf("authorization header missing signed headers or signature")
	}
	signedHeadersArr := strings.Split(signedHeaders, ";")

	amzDate := areq.Request.Header.Get("x-amz-date")
	if amzDate == "" {
		return accessKey, fmt.Errorf("missing x-amz-date")
	}

	scope := strings.Join([]string{date, region, service, "aws4_request"}, "/")
	digest := sha256.Sum256([]byte(canonicalRequest(areq.Request, signedHeadersArr)))
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		hex.EncodeToString(digest[:]),
	}, "\n")
	expected := hex.EncodeToString(hmacSHA256(sigV4SigningKey(secret, date, region, service), []byte(stringToSign)))
	if !hmac.Equal([]byte(expected), []byte(providedSignature)) {
		return accessKey, fmt.Errorf("signature mismatch")
	}

	areq.AuthorizedHeaders = make(http.Header)
	for _, s := range signedHeadersArr {
		if strings.EqualFold(s, "x-amz-content-sha256") {
			areq.ExpectedContentSha256 = areq.Request.Header.Get(s)
		}
		areq.AuthorizedHeaders.Add(s, areq.Request.Header.Get(s))
	}

	return accessKey, nil
}

func (areq *AwsRequest) GetBody() io.ReadCloser {
	return areq.body
}

func (areq *AwsRequest) ValidateContentHash() bool {
	return strings.EqualFold(areq.ExpectedContentSha256, hex.EncodeToString(areq.body.Digest()))
}
