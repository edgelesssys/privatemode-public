// Package ocsp provides types and constants for handling OCSP (Online Certificate Status Protocol) responses.
package ocsp

import (
	"fmt"
	"slices"
	"time"
)

// StatusValue is the string representation of the OCSP status.
type StatusValue string

const (
	good    StatusValue = "GOOD"
	unknown StatusValue = "UNKNOWN"
	revoked StatusValue = "REVOKED"
)

var (
	// StatusGood indicates that the certificate is not revoked.
	StatusGood = Status{Value: good, RevokedAt: time.Time{}}
	// StatusUnknown indicates that the OCSP server does not know about the certificate
	// or, contrary to the original OCSP specification, that the server is unreachable.
	StatusUnknown = Status{Value: unknown, RevokedAt: time.Time{}}
)

// Status represents the status of a certificate as returned by the OCSP server,
// including revocation date in case of revoked status.
type Status struct {
	Value     StatusValue
	RevokedAt time.Time `json:",omitzero"`
}

// StatusRevoked returns a Status indicating that the certificate was revoked at the given date.
func StatusRevoked(revokedAt time.Time) Status {
	return Status{Value: revoked, RevokedAt: revokedAt}
}

// AcceptedBy checks if the status is accepted by any of the allowed statuses.
// The status must not be revoked before the oldest allowed revocation date of an allowed status with the same value.
func (s Status) AcceptedBy(allowedStatuses []Status) bool {
	for _, allowed := range allowedStatuses {
		if allowed.Value == s.Value {
			if !s.RevokedAt.Before(allowed.RevokedAt) {
				return true
			}
		}
	}
	return false
}

// String returns a string representation of [Status].
func (s Status) String() string {
	switch s.Value {
	case good:
		return string(s.Value)
	case revoked:
		return fmt.Sprintf("%s at %s", s.Value, s.RevokedAt.Format(time.RFC3339))
	}
	return string(unknown)
}

// StatusInfo holds the OCSP status of the GPUs, VBIOS, and driver.
type StatusInfo struct {
	GPU    Status
	VBIOS  Status
	Driver Status
}

// CombineStatuses combines multiple [Status]es into a single [Status], with the
// precedence [StatusRevoked] > [StatusUnknown] > [StatusGood].
func CombineStatuses(status []Status) Status {
	// If any status is revoked, return revoked with the earliest revocation time
	var revokedDates []time.Time
	for _, s := range status {
		if s.Value == revoked {
			revokedDates = append(revokedDates, s.RevokedAt)
		}
	}
	if len(revokedDates) > 0 {
		// Return the revoked status with the earliest revocation date
		return StatusRevoked(slices.MinFunc(revokedDates, func(a, b time.Time) int {
			if a.Before(b) {
				return -1
			}
			if a.After(b) {
				return 1
			}
			return 0
		}))
	}

	// If any status is unknown, but none are revoked, return unknown
	if slices.ContainsFunc(status, func(s Status) bool {
		return s.Value == unknown
	}) {
		return StatusUnknown
	}
	// If all statuses are good, return good
	return StatusGood
}
