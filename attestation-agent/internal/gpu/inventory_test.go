//go:build gpu

package gpu

import (
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequest(t *testing.T) {
	testCases := map[string]struct {
		availableGPUs     []Device
		inUseGPUs         map[string][]Device
		user              string
		count             uint
		expected          []Device
		expectedAvailable []Device
		wantErr           bool
	}{
		"success, none in use": {
			availableGPUs: []Device{
				{id: "0"},
				{id: "1"},
				{id: "2"},
			},
			inUseGPUs: make(map[string][]Device),
			user:      "user",
			count:     2,
			expected: []Device{
				{id: "0"},
				{id: "1"},
			},
			expectedAvailable: []Device{
				{id: "2"},
			},
		},
		"success, some in use by other user": {
			availableGPUs: []Device{
				{id: "0"},
				{id: "2"},
			},
			inUseGPUs: map[string][]Device{
				"other-user": {
					{id: "1"},
				},
			},
			user:  "user",
			count: 2,
			expected: []Device{
				{id: "0"},
				{id: "2"},
			},
			expectedAvailable: []Device{},
		},
		"success, some in use by ourselves": {
			availableGPUs: []Device{
				{id: "0"},
				{id: "2"},
			},
			inUseGPUs: map[string][]Device{
				"user": {
					{id: "1"},
				},
			},
			user:  "user",
			count: 2,
			expected: []Device{
				{id: "1"},
				{id: "0"},
				{id: "2"},
			},
			expectedAvailable: []Device{},
		},
		"request more than available": {
			availableGPUs: []Device{
				{id: "0"},
				{id: "1"},
			},
			inUseGPUs: make(map[string][]Device),
			user:      "user",
			count:     3,
			wantErr:   true,
			expectedAvailable: []Device{
				{id: "0"},
				{id: "1"},
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			inventory := &Inventory{
				log:           slog.New(slog.NewTextHandler(os.Stderr, nil)),
				mut:           &sync.RWMutex{},
				availableGPUs: tc.availableGPUs,
				inUseGPUs:     tc.inUseGPUs,
			}

			gpus, err := inventory.Request(tc.user, tc.count)
			if tc.wantErr {
				require.Error(err)
			} else {
				require.NoError(err)
				assert.Equal(tc.expected, gpus)
				assert.Equal(tc.expectedAvailable, inventory.availableGPUs)
			}
		})
	}
}

func TestReleaseAll(t *testing.T) {
	testCases := map[string]struct {
		availableGPUs     []Device
		inUseGPUs         map[string][]Device
		user              string
		expectedAvailable []Device
		expectedInUse     map[string][]Device
	}{
		"success": {
			availableGPUs: []Device{
				{id: "0"},
				{id: "1"},
			},
			inUseGPUs: map[string][]Device{
				"user": {
					{id: "2"},
					{id: "3"},
				},
				"other-user": {
					{id: "4"},
				},
			},
			user: "user",
			expectedAvailable: []Device{
				{id: "0"},
				{id: "1"},
				{id: "2"},
				{id: "3"},
			},
			expectedInUse: map[string][]Device{
				"other-user": {
					{id: "4"},
				},
			},
		},
		"user has no GPUs in use": {
			availableGPUs: []Device{
				{id: "0"},
				{id: "1"},
			},
			inUseGPUs: map[string][]Device{"user": {}},
			user:      "user",
			expectedAvailable: []Device{
				{id: "0"},
				{id: "1"},
			},
			expectedInUse: make(map[string][]Device),
		},
		"user is not in inventory": {
			availableGPUs: []Device{
				{id: "0"},
				{id: "1"},
			},
			inUseGPUs: map[string][]Device{},
			user:      "user",
			expectedAvailable: []Device{
				{id: "0"},
				{id: "1"},
			},
			expectedInUse: map[string][]Device{},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			inventory := &Inventory{
				log:           slog.New(slog.NewTextHandler(os.Stderr, nil)),
				mut:           &sync.RWMutex{},
				availableGPUs: tc.availableGPUs,
				inUseGPUs:     tc.inUseGPUs,
			}

			inventory.ReleaseAll(tc.user)
			assert.Equal(tc.expectedAvailable, inventory.availableGPUs)
			assert.Equal(tc.expectedInUse, inventory.inUseGPUs)
		})
	}
}
