package licensedb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteOrganization(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	sut := setupTestDB(t)
	ctx := t.Context()

	org, err := sut.InsertOrganization(ctx, Organization{
		ClerkOrgID:       "test-org-id",
		RoleID:           1,
		StripeCustomerID: "test-stripe-customer-id",
	})
	require.NoError(err)

	// insert a license key
	require.NoError(sut.InsertLicenseEntry(ctx, LicenseEntry{
		LicenseKey:     "test-license-key",
		OrganizationID: org.ID,
		Organization:   Organization{},
	}))

	// insert to check that associated org data is available after deletion
	require.NoError(sut.InsertUsageEntries(ctx, []UsageEntry{
		{
			ID:             1,
			LicenseKey:     "test-license-key",
			ModelName:      "test-model-name",
			APIEndpoint:    "test-api-endpoint",
			OrganizationID: &org.ID,
			Organization:   nil,
		},
	}))

	// assert org was inserted
	org2, err := sut.GetOrgByClerkOrgID(ctx, "test-org-id")
	require.NoError(err)
	require.Equal(org.ID, org2.ID)

	_, err = sut.InsertOrganization(ctx, Organization{
		ClerkOrgID:       "another-org-id",
		RoleID:           1,
		StripeCustomerID: "another-stripe-customer-id",
	})
	require.NoError(err)

	assert.NoError(sut.DeleteOrganization(ctx, org.ID))

	// assert soft delete
	var deletedOrg Organization
	require.NoError(sut.GetGormDB().Unscoped().Where("id = ?", org.ID).First(&deletedOrg).Error)
	assert.Equal(deletedOrg.ClerkOrgID, org.ClerkOrgID)

	// assert org data for usage entries is still present
	var usageEntry UsageEntry
	err = sut.GetGormDB().Unscoped().Preload("Organization").Where("id = ?", 1).First(&usageEntry).Error
	require.NoError(err)
	require.NotNil(usageEntry.Organization)
	assert.Equal("test-org-id", usageEntry.Organization.ClerkOrgID)

	// assert org was deleted
	_, err = sut.GetOrgByClerkOrgID(ctx, "test-org-id")
	require.Error(err)

	// assert license key was deleted
	_, err = sut.GetLicenseEntryByLicenseKey(ctx, "test-license-key")
	require.Error(err)

	// assert another org still exists
	_, err = sut.GetOrgByClerkOrgID(ctx, "another-org-id")
	require.NoError(err)
}
