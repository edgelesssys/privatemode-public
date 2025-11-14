// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package ocspheader handles parsing & packing the Privatemode-OCSP-Allow-Status header.
package ocspheader

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// AllowStatus defines what [ocsp.Status]es are allowed, and is used in the
// Privatemode-OCSP-Allow-Status header.
type AllowStatus string

const (
	// AllowStatusGood indicates that [ocsp.StatusGood] is allowed.
	AllowStatusGood AllowStatus = "allow-good"
	// AllowStatusUnknown indicates that [ocsp.StatusUnknown] is allowed.
	AllowStatusUnknown AllowStatus = "allow-unknown"
	// AllowStatusRevoked indicates that [ocsp.StatusRevoked] is allowed.
	AllowStatusRevoked AllowStatus = "allow-revoked"
)

// String returns the string representation of the [AllowStatus].
func (s AllowStatus) String() string {
	return string(s)
}

// Header is the structured representation of the Privatemode-OCSP-Allow-Status header.
type Header struct {
	AllowedStatuses []AllowStatus
	RevokedNbf      time.Time
}

// NewHeader builds the Privatemode-OCSP-Allow-Status header from the given allowed status codes,
// grace period, and revocation time.
func NewHeader(allowedStatuses []AllowStatus, revocNbf time.Time) *Header {
	return &Header{
		AllowedStatuses: allowedStatuses,
		RevokedNbf:      revocNbf,
	}
}

// Marshal converts the Header into a string representation suitable for the Privatemode-OCSP-Allow-Status header.
func (h *Header) Marshal() (string, error) {
	if len(h.AllowedStatuses) < 1 {
		return "", fmt.Errorf("at least one allowed OCSP status must be specified")
	}

	statusStrings := make([]string, len(h.AllowedStatuses))
	for i, status := range h.AllowedStatuses {
		statusStrings[i] = status.String()
	}
	allowedStatuses := strings.Join(statusStrings, ",")

	unixTimeStr := fmt.Sprintf("%d", h.RevokedNbf.Unix())

	return strings.Join([]string{
		fmt.Sprintf("rules=%s", allowedStatuses),
		fmt.Sprintf("revocation-time-not-before=%s", unixTimeStr),
	}, "; "), nil
}

// MarshalMACHeader computes the HMAC of the header contents using the provided secret.
func (h *Header) MarshalMACHeader(secret [32]byte) (string, error) {
	marshaled, err := h.Marshal()
	if err != nil {
		return "", fmt.Errorf("error marshaling header: %w", err)
	}

	hmac := hmac.New(sha256.New, secret[:])
	hmac.Write([]byte(marshaled))
	mac := hmac.Sum(nil)

	return hex.EncodeToString(mac), nil
}

// UnmarshalAndVerify parses the [constants.] string and verifies its integrity.
func UnmarshalAndVerify(policyHeader, policyMACHeader string, secret [32]byte) (*Header, error) {
	parts := strings.Split(policyHeader, "; ")
	expectedPartCount := 2
	if len(parts) != expectedPartCount {
		return nil, fmt.Errorf("invalid header format: expected %d parts, got %d", expectedPartCount, len(parts))
	}

	// As e.g. revocNbf doesn't have a meaningful zero value for us to
	// check if we parsed it, but we still want to require that it was specified,
	// we need to have explicit out-of-band info that we parsed all parts correctly.
	var rulesParsed, revocNbfParsed bool
	var rules string
	var revocNbf time.Time
	for _, part := range parts {
		switch {
		case strings.HasPrefix(part, "rules="):
			rules = strings.TrimPrefix(part, "rules=")
			rulesParsed = true
		case strings.HasPrefix(part, "revocation-time-not-before="):
			var unix int64
			_, err := fmt.Sscanf(part, "revocation-time-not-before=%d", &unix)
			if err != nil {
				return nil, fmt.Errorf("parsing revocation time not before: %w", err)
			}
			revocNbf = time.Unix(unix, 0)
			revocNbfParsed = true
		default:
			return nil, fmt.Errorf("unknown part in header: %s", part)
		}
	}
	if !rulesParsed || !revocNbfParsed {
		return nil, fmt.Errorf("missing required parts in header")
	}

	allowedStatusStrings := strings.Split(rules, ",")
	if len(allowedStatusStrings) == 0 {
		return nil, fmt.Errorf("no allowed OCSP status found")
	}

	allowedStatuses := make([]AllowStatus, 0, len(allowedStatusStrings))
	for _, statusStr := range allowedStatusStrings {
		status, err := allowStatusFromString(statusStr)
		if err != nil {
			return nil, fmt.Errorf("parsing OCSP status: %w", err)
		}
		allowedStatuses = append(allowedStatuses, status)
	}

	header := NewHeader(allowedStatuses, revocNbf)
	expectedMac, err := header.MarshalMACHeader(secret)
	if err != nil {
		return nil, fmt.Errorf("error generating expected MAC: %w", err)
	}

	if expectedMac != policyMACHeader {
		return nil, fmt.Errorf("HMAC verification failed: expected %s, got %s",
			expectedMac, policyMACHeader)
	}

	return header, nil
}

// allowStatusFromString converts a string representation of an OCSP allow status to the [AllowStatus] type.
func allowStatusFromString(status string) (AllowStatus, error) {
	switch strings.ToLower(status) {
	case string(AllowStatusGood):
		return AllowStatusGood, nil
	case string(AllowStatusUnknown):
		return AllowStatusUnknown, nil
	case string(AllowStatusRevoked):
		return AllowStatusRevoked, nil
	default:
		return "", fmt.Errorf("invalid OCSP allow status: %s", status)
	}
}
