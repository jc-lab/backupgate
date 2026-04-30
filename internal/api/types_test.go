// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/xml"
	"testing"
)

func TestBucketLocationResultMarshal(t *testing.T) {
	got, err := xml.Marshal(NewBucketLocationResult())
	if err != nil {
		t.Fatalf("marshaling BucketLocationResult: %v", err)
	}
	want := []byte(`<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">dummy</LocationConstraint>`)
	if !bytes.Equal(got, want) {
		t.Fatalf("marshal result = %q, want %q", got, want)
	}
}

func TestS3ErrorResultMarshal(t *testing.T) {
	got, err := xml.Marshal(S3ErrorResult{
		Code:    "NoSuchKey",
		Message: "missing object",
	})
	if err != nil {
		t.Fatalf("marshaling S3ErrorResult: %v", err)
	}
	want := []byte(`<Error><Code>NoSuchKey</Code><Message>missing object</Message></Error>`)
	if !bytes.Equal(got, want) {
		t.Fatalf("marshal result = %q, want %q", got, want)
	}
}
