// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package forwarder

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStatusFromError(t *testing.T) {
	testCases := map[string]struct {
		err        error
		wantStatus int
	}{
		"nil error": {
			wantStatus: http.StatusOK,
		},
		"ordinary error": {
			err:        assert.AnError,
			wantStatus: http.StatusInternalServerError,
		},
		"forwarder error": {
			err:        NewError("validating request", assert.AnError, http.StatusBadRequest),
			wantStatus: http.StatusBadRequest,
		},
		"wrapped forwarder error": {
			err:        fmt.Errorf("mutating request: %w", NewError("validating request", assert.AnError, http.StatusBadRequest)),
			wantStatus: http.StatusBadRequest,
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.wantStatus, StatusFromError(tc.err))
		})
	}
}
