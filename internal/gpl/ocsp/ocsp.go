// Package ocsp provides types and constants for handling OCSP (Online Certificate Status Protocol) responses.
package ocsp

import (
	"fmt"
	"strings"
)

// Status represents the status of a certificate as returned by the OCSP server.
type Status string

const (
	// StatusGood indicates that the certificate not revoked.
	StatusGood Status = "GOOD"
	// StatusRevoked indicates that the certificate has been revoked.
	StatusRevoked Status = "REVOKED"
	// StatusUnknown indicates that the OCSP server does not know about the certificate
	// or, contrary to the original OCSP specification, that the server is unreachable.
	StatusUnknown Status = "UNKNOWN"
)

// StatusInfo holds the OCSP status of the GPUs, VBIOS, and driver.
type StatusInfo struct {
	GPU    Status
	VBIOS  Status
	Driver Status
}

// StatusFromString converts a string representation of an OCSP status to the Status type.
func StatusFromString(status string) (Status, error) {
	switch strings.ToUpper(status) {
	case string(StatusGood):
		return StatusGood, nil
	case string(StatusRevoked):
		return StatusRevoked, nil
	case string(StatusUnknown):
		return StatusUnknown, nil
	default:
		return "", fmt.Errorf("invalid OCSP status: %s", status)
	}
}

// CombineStatuses combines multiple [Status]es into a single [Status], with the
// precedence [StatusRevoked] > [StatusUnknown] > [StatusGood].
func CombineStatuses(status []Status) Status {
	// If any status is revoked, return revoked
	for _, s := range status {
		if s == StatusRevoked {
			return StatusRevoked
		}
	}
	// If any status is unknown, but none are revoked, return unknown
	for _, s := range status {
		if s == StatusUnknown {
			return StatusUnknown
		}
	}
	// If all statuses are good, return good
	return StatusGood
}
