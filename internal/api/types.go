// Copyright 2026 JC-Lab
// SPDX-License-Identifier: AGPL-3.0-only

package api

import "encoding/xml"

const xmlHeader = `<?xml version="1.0" encoding="UTF-8"?>`

type BucketLocationResult struct {
	XMLName            xml.Name `xml:"LocationConstraint"`
	XmlNs              string   `xml:"xmlns,attr"`
	LocationConstraint string   `xml:",chardata"`
}

func NewBucketLocationResult() *BucketLocationResult {
	return &BucketLocationResult{
		XmlNs:              "http://s3.amazonaws.com/doc/2006-03-01/",
		LocationConstraint: "dummy",
	}
}

type S3ErrorResult struct {
	XMLName xml.Name `xml:"Error"`
	Code    string   `xml:"Code"`
	Message string   `xml:"Message"`
}
