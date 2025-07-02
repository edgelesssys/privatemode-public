package licensedb

import (
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTotalUsageInPeriodByOrganization(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	db := setupTestDB(t)

	org1 := uint(1)
	org2 := uint(2)

	startDate := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)

	// Fill DB with test data
	usageEntries := []UsageEntry{
		{
			LicenseKey:         "license1",
			OrganizationID:     &org1,
			CachedPromptTokens: 100,
			PromptTokens:       100,
			CompletionTokens:   100,
			Timestamp:          startDate.Add(time.Hour),
		},
		{
			LicenseKey:         "license2",
			OrganizationID:     &org1,
			CachedPromptTokens: 100,
			PromptTokens:       100,
			CompletionTokens:   100,
			Timestamp:          startDate.Add(time.Hour * 2),
		},
		{
			LicenseKey:         "license3",
			OrganizationID:     &org1,
			CachedPromptTokens: 100,
			PromptTokens:       100,
			CompletionTokens:   100,
			Timestamp:          startDate.Add(time.Hour * 3),
		},
		{
			LicenseKey:         "license4",
			OrganizationID:     &org2,
			CachedPromptTokens: 50,
			PromptTokens:       50,
			CompletionTokens:   50,
			Timestamp:          startDate.Add(time.Hour * 4),
		},
	}

	require.NoError(db.InsertUsageEntries(t.Context(), usageEntries))

	totalUsage, err := db.GetTotalUsageInPeriodByOrganization(t.Context(), startDate, startDate.Add(time.Hour*24))
	assert.NoError(err)
	assert.Len(totalUsage, 2)

	for _, entry := range totalUsage {
		switch entry.OrganizationID {
		case &org1:
			assert.Equal(int64(300), entry.CachedPromptTokens, "Cached prompt tokens for org1 should be 300")
			assert.Equal(int64(300), entry.PromptTokens, "Prompt tokens for org1 should be 300")
			assert.Equal(int64(300), entry.CompletionTokens, "Completion tokens for org1 should be 300")
		case &org2:
			assert.Equal(int64(50), entry.CachedPromptTokens, "Cached prompt tokens for org2 should be 50")
			assert.Equal(int64(50), entry.PromptTokens, "Prompt tokens for org2 should be 50")
			assert.Equal(int64(50), entry.CompletionTokens, "Completion tokens for org2 should be 50")
		}
	}
}

func TestGetUsageByMonth(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	sut := setupTestDB(t)
	ctx := t.Context()

	// Test parameters
	orgID := uint(1)
	yearMonth := "2025-05"
	groupBy := GroupByAPIKey

	// Insert test data
	testData := []UsageEntry{
		{
			LicenseKey:         "license1",
			OrganizationID:     &orgID,
			ModelName:          "ibnzterrell/Meta-Llama-3.3-70B-Instruct-AWQ-INT4",
			APIEndpoint:        "/v1/chat/completions",
			Timestamp:          time.Date(2025, 5, 1, 10, 0, 0, 0, time.UTC),
			PromptTokens:       100,
			CachedPromptTokens: 50,
			CompletionTokens:   200,
		},
		{
			LicenseKey:         "license1",
			OrganizationID:     &orgID,
			ModelName:          "ibnzterrell/Meta-Llama-3.3-70B-Instruct-AWQ-INT4",
			APIEndpoint:        "/v1/chat/completions",
			Timestamp:          time.Date(2025, 5, 2, 10, 0, 0, 0, time.UTC),
			PromptTokens:       150,
			CachedPromptTokens: 25,
			CompletionTokens:   250,
		},
		{
			LicenseKey:         "license1",
			OrganizationID:     &orgID,
			ModelName:          "intfloat/multilingual-e5-large-instruct",
			APIEndpoint:        "/v1/embeddings",
			Timestamp:          time.Date(2025, 5, 1, 10, 0, 0, 0, time.UTC),
			PromptTokens:       500,
			CachedPromptTokens: 0,
			CompletionTokens:   0,
		},
		{
			LicenseKey:         "license2",
			OrganizationID:     &orgID,
			ModelName:          "leon-se/gemma-3-27b-it-fp8-dynamic",
			APIEndpoint:        "/v1/chat/completions",
			Timestamp:          time.Date(2025, 5, 1, 10, 10, 0, 0, time.UTC),
			PromptTokens:       80,
			CachedPromptTokens: 0,
			CompletionTokens:   120,
		},
		{
			LicenseKey:         "license2",
			OrganizationID:     &orgID,
			ModelName:          "leon-se/gemma-3-27b-it-fp8-dynamic",
			APIEndpoint:        "/v1/chat/completions",
			Timestamp:          time.Date(2025, 5, 3, 10, 10, 0, 0, time.UTC),
			PromptTokens:       90,
			CachedPromptTokens: 90,
			CompletionTokens:   110,
		},
	}
	for _, entry := range testData {
		err := sut.db.Create(&entry).Error
		require.NoError(err)
	}

	dailyUsage, err := sut.GetUsageByMonth(ctx, orgID, yearMonth, groupBy, "/v1/chat/completions")
	require.NoError(err)

	// Create a map to organize the results by license key and day
	usageByKeyAndDay := make(map[string]map[int]DailyUsage)
	for _, entry := range dailyUsage {
		if _, exists := usageByKeyAndDay[entry.GroupKey]; !exists {
			usageByKeyAndDay[entry.GroupKey] = make(map[int]DailyUsage)
		}
		usageByKeyAndDay[entry.GroupKey][entry.Day] = entry
	}

	// Verify we have data for both license keys
	assert.Len(usageByKeyAndDay, 2)
	license1Data, hasLicense1 := usageByKeyAndDay["license1"]
	license2Data, hasLicense2 := usageByKeyAndDay["license2"]
	require.True(hasLicense1, "Missing data for license1")
	require.True(hasLicense2, "Missing data for license2")

	// Verify license1 data
	day1Data, hasDay1 := license1Data[1] // Day 1
	require.True(hasDay1, "Missing data for license1 on day 1")
	assert.Equal(100, day1Data.PromptTokens)
	assert.Equal(50, day1Data.CachedPromptTokens)
	assert.Equal(200, day1Data.CompletionTokens)
	assert.Equal(300, day1Data.TotalTokens)

	day2Data, hasDay2 := license1Data[2] // Day 2
	require.True(hasDay2, "Missing data for license1 on day 2")
	assert.Equal(150, day2Data.PromptTokens)
	assert.Equal(25, day2Data.CachedPromptTokens)
	assert.Equal(250, day2Data.CompletionTokens)
	assert.Equal(400, day2Data.TotalTokens)

	// Verify license2 data
	day1DataL2, hasDay1L2 := license2Data[1] // Day 1
	require.True(hasDay1L2, "Missing data for license2 on day 1")
	assert.Equal(80, day1DataL2.PromptTokens)
	assert.Equal(0, day1DataL2.CachedPromptTokens)
	assert.Equal(120, day1DataL2.CompletionTokens)
	assert.Equal(200, day1DataL2.TotalTokens)

	day3DataL2, hasDay3L2 := license2Data[3] // Day 3
	require.True(hasDay3L2, "Missing data for license2 on day 3")
	assert.Equal(90, day3DataL2.PromptTokens)
	assert.Equal(90, day3DataL2.CachedPromptTokens)
	assert.Equal(110, day3DataL2.CompletionTokens)
	assert.Equal(200, day3DataL2.TotalTokens)

	// Test embedding
	embedUsage, err := sut.GetUsageByMonth(ctx, orgID, yearMonth, groupBy, "/v1/embeddings")
	require.NoError(err)
	assert.Len(embedUsage, 1)
	assert.Equal(500, embedUsage[0].PromptTokens)
	assert.Equal(0, embedUsage[0].CachedPromptTokens)
	assert.Equal(0, embedUsage[0].CompletionTokens)
	assert.Equal(500, embedUsage[0].TotalTokens)
}
