package licensedb

import (
	"context"
	"fmt"
)

// AutoMigrate creates or updates database tables to match the current model definitions.
// This is generally safe to run in development but should be used with caution in production.
func (l *LicenseDB) AutoMigrate(ctx context.Context) error {
	// Use WithContext to ensure the migration respects context deadlines/cancellation
	db := l.db.WithContext(ctx)

	// Migrate the ModelEndpointPairing table
	if err := db.AutoMigrate(&ModelEndpointPairing{}); err != nil { //nolint:exhaustruct
		return fmt.Errorf("migrating model_endpoint_pairings table: %w", err)
	}

	// Migrate the Role table
	if err := db.AutoMigrate(&Role{}); err != nil { //nolint:exhaustruct
		return fmt.Errorf("migrating roles table: %w", err)
	}

	// Migrate the Organization table
	if err := db.AutoMigrate(&Organization{}); err != nil { //nolint:exhaustruct
		return fmt.Errorf("migrating organizations table: %w", err)
	}

	// Migrate the LicenseEntry table
	if err := db.AutoMigrate(&LicenseEntry{}); err != nil { //nolint:exhaustruct
		return fmt.Errorf("migrating license_info table: %w", err)
	}

	// Migrate the UsageEntry table
	if err := db.AutoMigrate(&UsageEntry{}); err != nil { //nolint:exhaustruct
		return fmt.Errorf("migrating token_usage table: %w", err)
	}

	return nil
}
