//go:build integration && gcp_db

package licensedb

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/mattn/go-sqlite3" // SQLite driver
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testLicenseKey           = "11111111-2222-3333-4444-555555555555"
	testModelName            = "llama3.3"
	testAPIEndpoint          = "/v1/chat/completions"
	testOrgID                = uint(3)
	expectedPromptTokens     = 1000
	expectedCompletionTokens = 2000
)

var testDays = []int{1, 5, 10, 15, 20, 25, 28}

func TestGetUsageByMonthWithMySQL(t *testing.T) {
	ctx := context.Background()
	userName := os.Getenv("DATABASE_USER_NAME")
	require.NotEmpty(t, userName, "SQL user name not set")
	databaseName := "continuum-license-testing"
	sqlConnectionString := "constellation-license-server:europe-west1:production-license-db"

	licenseDB, err := New(ctx, userName, databaseName, sqlConnectionString, slog.Default())
	require.NoError(t, err)

	addFakeTokenUsage(t, licenseDB)

	yearMonth := "2025-05" // refers to entries from seed-db

	// Test 1: Query by API key
	t.Run("QueryByAPIKey", func(t *testing.T) {
		dailyUsageAPIKey, err := licenseDB.GetUsageByMonth(ctx, testOrgID, yearMonth, GroupByAPIKey, []string{testAPIEndpoint})
		require.NoError(t, err)
		verifyUsageGroup(t, dailyUsageAPIKey, "license_key", testLicenseKey, testDays)
	})

	// Test 2: Query by model name
	t.Run("QueryByModelName", func(t *testing.T) {
		dailyUsageModel, err := licenseDB.GetUsageByMonth(ctx, testOrgID, yearMonth, GroupByModel, []string{testModelName})
		require.NoError(t, err)
		verifyUsageGroup(t, dailyUsageModel, "model_name", testModelName, testDays)
	})
}

func verifyUsageGroup(t *testing.T, dailyUsage []DailyUsage, groupKeyType string, expectedGroupKey string, expectedDays []int) {
	t.Helper()

	usageByGroupKey := make(map[string][]DailyUsage)
	for _, entry := range dailyUsage {
		usageByGroupKey[entry.GroupKey] = append(usageByGroupKey[entry.GroupKey], entry)
	}

	assert.Len(t, usageByGroupKey, 1, "Should have data for 1 "+groupKeyType)
	assert.Contains(t, usageByGroupKey, expectedGroupKey, expectedGroupKey+" should be in the results")

	entries := usageByGroupKey[expectedGroupKey]
	dayToEntry := make(map[int]DailyUsage)
	for _, entry := range entries {
		dayToEntry[entry.Day] = entry
	}

	expectedTotalTokens := expectedPromptTokens + expectedCompletionTokens

	for _, day := range expectedDays {
		entry, exists := dayToEntry[day]
		require.True(t, exists, "Missing data for "+groupKeyType+" "+expectedGroupKey+" on day "+strconv.Itoa(day))

		assert.Equal(t, expectedPromptTokens, entry.PromptTokens, "Prompt tokens for "+groupKeyType+" "+expectedGroupKey+" on day "+strconv.Itoa(day))
		assert.Equal(t, expectedCompletionTokens, entry.CompletionTokens, "Completion tokens for "+groupKeyType+" "+expectedGroupKey+" on day "+strconv.Itoa(day))
		assert.Equal(t, expectedTotalTokens, entry.TotalTokens, "Total tokens for "+groupKeyType+" "+expectedGroupKey+" on day "+strconv.Itoa(day))
	}
}

// addFakeTokenUsage adds a specific set of fake token usage data to the database.
func addFakeTokenUsage(t *testing.T, db *LicenseDB) {
	ctx := t.Context()
	// Define the fixed options for seeding directly within the function
	year := 2025
	month := 5

	var entries []UsageEntry

	for _, day := range testDays {
		timestamp := time.Date(year, time.Month(month), day, 12, 0, 0, 0, time.UTC)
		entry := UsageEntry{
			LicenseKey:       testLicenseKey,
			OrganizationID:   func(i uint) *uint { return &i }(testOrgID),
			ModelName:        testModelName,
			APIEndpoint:      testAPIEndpoint,
			Timestamp:        timestamp,
			PromptTokens:     int64(expectedPromptTokens),
			CompletionTokens: int64(expectedCompletionTokens),
		}
		entries = append(entries, entry)
	}

	if err := db.InsertUsageEntries(ctx, entries); err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			t.Logf("Fake usage data already exists, skipping")
		} else {
			assert.Fail(t, "Failed to insert fake token usage entry: %v", err)
		}
	}
}
