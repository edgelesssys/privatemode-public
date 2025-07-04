package licensedb

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var mockEntry = LicenseEntry{
	LicenseKey:       "00000000-0000-0000-0000-000000000000",
	OrganizationName: "Test Org",
	IssueDate:        time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	OrganizationID:   1,
	Organization: Organization{
		Model: gorm.Model{
			ID:        1,
			CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			DeletedAt: gorm.DeletedAt{},
		},
		ClerkOrgID:       "clerk_org_id",
		StripeCustomerID: "stripe_customer_id",
		RoleID:           1,
		Role: Role{
			ID: 1,
			ModelEndpointPairings: []ModelEndpointPairing{
				{
					ModelName:   "llama3.3",
					APIEndpoint: "/v1/chat/completions",
				},
			},
			MonthlyPromptTokens:       1000,
			MonthlyCompletionTokens:   1000,
			MonthlyFileSizeMB:         1024,
			PromptTokensPerMinute:     100,
			CompletionTokensPerMinute: 50,
			FileSizeMBPerMinute:       10,
			RequestsPerMinute:         10,
		},
	},
	ExpirationDate:            time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	UsageLimit:                10,
	PromptTokensPerMinute:     100,
	CompletionTokensPerMinute: 50,
	RequestsPerMinute:         10,
	StripeCustomerID:          nil,
	Type:                      "Test Type",
	Comment:                   "Test Comment",
}

func TestInsertLicense(t *testing.T) {
	require := require.New(t)

	sut := setupTestDB(t)
	ctx := t.Context()

	require.NoError(sut.InsertLicenseEntry(ctx, mockEntry))
	retrieved, err := sut.GetLicenseEntryByLicenseKey(ctx, mockEntry.LicenseKey)
	require.NoError(err)
	require.Equal(mockEntry, retrieved)
}

func TestUpdateLicenseEntry(t *testing.T) {
	require := require.New(t)

	sut := setupTestDB(t)
	ctx := t.Context()

	require.NoError(sut.InsertLicenseEntry(ctx, mockEntry))

	updatedOrg := "Test Org 2"
	var updatedUsage int64 = 2
	entry := UpdateLicenseEntry{
		LicenseKey:   mockEntry.LicenseKey,
		Organization: &updatedOrg,
		UsageLimit:   &updatedUsage,
	}

	require.NoError(sut.UpdateLicenseEntry(ctx, entry))
	retrieved, err := sut.GetLicenseEntryByLicenseKey(ctx, mockEntry.LicenseKey)
	require.NoError(err)
	require.Equal(updatedOrg, retrieved.OrganizationName)
	require.Equal(updatedUsage, retrieved.UsageLimit)
}

func setupTestDB(t *testing.T) *LicenseDB {
	require := require.New(t)

	// Create an in-memory SQLite database for testing
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared")
	t.Cleanup(func() {
		db.Close()
	})
	require.NoError(err)

	ldb, err := NewFromSQLDatabase(DialectSQLite, db)
	require.NoError(err)
	require.NoError(ldb.AutoMigrate(t.Context()))

	return ldb
}
