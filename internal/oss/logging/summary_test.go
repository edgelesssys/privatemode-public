// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package logging

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSummaryAttrsRoundtrip(t *testing.T) {
	assert := assert.New(t)

	ctx, attrs := WithSummaryAttrs(context.Background())
	AddSummaryAttrs(ctx, "a", 1)
	AddSummaryAttrs(ctx, "b", 2, "c", 3)

	// No-op on non-enriched context
	AddSummaryAttrs(context.Background(), "d", 4)

	assert.Equal([]any{"a", 1, "b", 2, "c", 3}, attrs.Args())
}
