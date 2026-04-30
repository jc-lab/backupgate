// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

func TestS3GetBucketLocation(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/backupgate/?location", nil)
	rec := httptest.NewRecorder()

	s := NewS3Handler(nil)
	s.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/xml" {
		t.Fatalf("Content-Type = %q, want application/xml", got)
	}

	requestID := resp.Header.Get("x-amz-request-id")
	if requestID == "" {
		t.Fatal("x-amz-request-id header is empty")
	}
	uuidPattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !uuidPattern.MatchString(requestID) {
		t.Fatalf("x-amz-request-id = %q, want UUID v4", requestID)
	}

	var result BucketLocationResult
	if err := xml.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding XML response: %v", err)
	}
	if result.XMLName.Local != "LocationConstraint" {
		t.Fatalf("XML root = %q, want LocationConstraint", result.XMLName.Local)
	}
	if result.LocationConstraint != "dummy" {
		t.Fatalf("LocationConstraint = %q, want dummy", result.LocationConstraint)
	}
}

func TestWriteS3Error(t *testing.T) {
	rec := httptest.NewRecorder()

	writeS3Error(rec, http.StatusNotFound, "NoSuchKey", "missing object")

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/xml" {
		t.Fatalf("Content-Type = %q, want application/xml", got)
	}

	var result S3ErrorResult
	if err := xml.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding XML response: %v", err)
	}
	if result.XMLName.Local != "Error" {
		t.Fatalf("XML root = %q, want Error", result.XMLName.Local)
	}
	if result.Code != "NoSuchKey" || result.Message != "missing object" {
		t.Fatalf("error result = %#v, want code NoSuchKey and message missing object", result)
	}
}
