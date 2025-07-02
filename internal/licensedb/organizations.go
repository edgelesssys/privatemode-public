package licensedb

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// Organization represents a customer organization.
type Organization struct {
	gorm.Model
	ClerkOrgID       string `json:"clerk_org_id" gorm:"uniqueIndex;size:255"`
	StripeCustomerID string `json:"stripe_customer_id" gorm:"unique;size:255"`
	RoleID           uint   `json:"role_id" gorm:"not null"` // Foreign key to [Roles] table
	Role             Role   `json:"role"`
}

// GetOrgByClerkOrgID retrieves an [Organization] by its Clerk organization ID from the database.
func (l *LicenseDB) GetOrgByClerkOrgID(ctx context.Context, clerkOrgID string) (Organization, error) {
	var org Organization
	result := l.db.WithContext(ctx).Where("clerk_org_id = ?", clerkOrgID).Preload("Role.ModelEndpointPairings.Billing").First(&org)
	if result.Error != nil {
		return org, fmt.Errorf("getting org by Clerk org ID %q: %w", clerkOrgID, result.Error)
	}
	return org, nil
}

// InsertOrganization creates a new [Organization] in the database.
func (l *LicenseDB) InsertOrganization(ctx context.Context, org Organization) (Organization, error) {
	result := l.db.WithContext(ctx).Create(&org)
	if result.Error != nil {
		return org, fmt.Errorf("inserting org: %w", result.Error)
	}
	return org, nil
}

// UpdateOrgRole updates the role of an [Organization] in the database.
func (l *LicenseDB) UpdateOrgRole(ctx context.Context, orgID uint, roleID uint) error {
	var org Organization
	result := l.db.WithContext(ctx).Model(&org).Where("id = ?", orgID).Update("role_id", roleID)
	if result.Error != nil {
		return fmt.Errorf("updating org role: %w", result.Error)
	}
	return nil
}

// DeleteOrganization deletes an [Organization] from the database.
func (l *LicenseDB) DeleteOrganization(ctx context.Context, orgID uint) error {
	var org Organization
	tx := l.db.Begin() // since Organizations are soft deleted, Cascade delete is not possible.
	result := tx.WithContext(ctx).Where("id = ?", orgID).Delete(&org)
	if result.Error != nil {
		return fmt.Errorf("deleting org with ID %d: %w", orgID, result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("no organization found with ID %d", orgID)
	}

	keysResult := tx.WithContext(ctx).Where("organization_id = ?", orgID).Delete(&LicenseEntry{}) // nolint:exhaustruct
	if keysResult.Error != nil {
		return fmt.Errorf("deleting license keys: %w", keysResult.Error)
	}

	return tx.Commit().Error
}

// UpdateOrganization updates an existing [Organization] in the database.
// Updates all fields including Role associations.
func (l *LicenseDB) UpdateOrganization(ctx context.Context, org Organization) error {
	result := l.db.WithContext(ctx).Save(&org)
	if result.Error != nil {
		return fmt.Errorf("updating organization %q: %w", org.ClerkOrgID, result.Error)
	}
	return nil
}
