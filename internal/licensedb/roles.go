package licensedb

import (
	"context"
	"fmt"
	"strings"
)

const (
	// RolesTable is the name of the table holding roles assignable to a license.
	RolesTable = "roles"
)

// Role is a role assigned to a license, providing access to a set of models and endpoints.
// A Role is granted to a [LicenseEntry] to provide access to [ModelEndpointPairing]s.
type Role struct {
	ID                        uint                   `gorm:"primaryKey;autoIncrement"`
	Name                      string                 `gorm:"unique;type:varchar(255);not null"`
	ModelEndpointPairings     []ModelEndpointPairing `gorm:"many2many:allowed_model_endpoint_pairings;foreignKey:id;joinForeignKey:role_id;references:api_endpoint,model_name;joinReferences:api_endpoint,model_name;constraint:OnDelete:CASCADE"`
	PromptTokensPerMinute     int64                  `gorm:"column:prompt_tokens_per_minute;type:BIGINT;not null;default:20000"`
	CompletionTokensPerMinute int64                  `gorm:"column:completion_tokens_per_minute;type:BIGINT;not null;default:10000"`
	FileSizeMBPerMinute       int64                  `gorm:"column:file_size_mb_per_minute;type:BIGINT;not null"`
	RequestsPerMinute         int64                  `gorm:"column:requests_per_minute;type:BIGINT;not null;default:20"`
	MonthlyPromptTokens       int64                  `gorm:"column:monthly_prompt_tokens;type:BIGINT;not null;default:300000"`
	MonthlyCompletionTokens   int64                  `gorm:"column:monthly_completion_tokens;type:BIGINT;not null;default:200000"`
	MonthlyFileSizeMB         int64                  `gorm:"column:monthly_file_size_mb;type:BIGINT;not null"`
}

// TableName returns the name of the table for the Role model.
func (Role) TableName() string {
	return RolesTable
}

// TableHeader returns the column names for the Role table.
func (Role) TableHeader() string {
	return "ID\tName\tModel-Endpoint Pairings"
}

// String returns the role as a tab separated string.
func (r Role) String() string {
	var modelEndpointPairings []string
	for _, pairing := range r.ModelEndpointPairings {
		modelEndpointPairings = append(modelEndpointPairings, pairing.String())
	}
	return fmt.Sprintf(
		"%d\t%s\t%s",
		r.ID, r.Name,
		strings.Join(modelEndpointPairings, ","),
	)
}

// Slice returns the role as a slice of strings, suitable for CSV output.
func (r Role) Slice() []string {
	var modelEndpointPairings []string
	for _, pairing := range r.ModelEndpointPairings {
		modelEndpointPairings = append(modelEndpointPairings, pairing.String())
	}
	return []string{
		fmt.Sprintf("%d", r.ID),
		r.Name,
		strings.Join(modelEndpointPairings, ","),
	}
}

// AllowsModelEndpointPairing checks if the role allows access to a specific model and API endpoint.
func (r Role) AllowsModelEndpointPairing(modelName, apiEndpoint string) bool {
	for _, pairing := range r.ModelEndpointPairings {
		if pairing.ModelName == modelName && pairing.APIEndpoint == apiEndpoint {
			return true
		}
	}
	return false
}

// GetRoles retrieves all [Role]s from the database.
func (l *LicenseDB) GetRoles(ctx context.Context) ([]Role, error) {
	var roles []Role
	result := l.db.WithContext(ctx).Preload("ModelEndpointPairings.Billing").Find(&roles)
	if result.Error != nil {
		return nil, fmt.Errorf("getting roles: %w", result.Error)
	}
	return roles, nil
}

// GetRoleByName retrieves a [Role] by its name from the database.
func (l *LicenseDB) GetRoleByName(ctx context.Context, roleName string) (Role, error) {
	var role Role
	result := l.db.WithContext(ctx).Where("name = ?", roleName).Preload("ModelEndpointPairings.Billing").First(&role)
	if result.Error != nil {
		return role, fmt.Errorf("getting role by name %q: %w", roleName, result.Error)
	}
	return role, nil
}

// GetRoleByID retrieves a [Role] by its ID from the database.
func (l *LicenseDB) GetRoleByID(ctx context.Context, roleID int) (Role, error) {
	var role Role
	result := l.db.WithContext(ctx).Where("id = ?", roleID).Preload("ModelEndpointPairings.Billing").First(&role)
	if result.Error != nil {
		return role, fmt.Errorf("getting role by ID %d: %w", roleID, result.Error)
	}
	return role, nil
}

// InsertRole inserts a new [Role] into the database.
// Creates any [ModelEndpointPairing] that don't already exist.
func (l *LicenseDB) InsertRole(ctx context.Context, role Role) (Role, error) {
	result := l.db.WithContext(ctx).Create(&role)
	if result.Error != nil {
		return role, fmt.Errorf("inserting role %q: %w", role.Name, result.Error)
	}
	return role, nil
}

// UpdateRole updates an existing [Role] in the database.
// Updates all fields including ModelEndpointPairings associations.
func (l *LicenseDB) UpdateRole(ctx context.Context, role Role) error {
	result := l.db.WithContext(ctx).Save(&role)
	if result.Error != nil {
		return fmt.Errorf("updating role %q: %w", role.Name, result.Error)
	}
	return nil
}

// AddModelEndpointPairingsToRole adds new [ModelEndpointPairing]s to a [Role].
// Creates any [ModelEndpointPairing] that don't already exist.
func (l *LicenseDB) AddModelEndpointPairingsToRole(ctx context.Context, roleID uint, pairings []ModelEndpointPairing) error {
	err := l.db.WithContext(ctx).Model(&Role{ID: roleID}).Association("ModelEndpointPairings").Append(pairings) //nolint:exhaustruct
	if err != nil {
		return fmt.Errorf("updating associations for role %d: %w", roleID, err)
	}
	return nil
}

// RemoveModelEndpointPairingsFromRole removes [ModelEndpointPairing]s from a [Role].
// [ModelEndpointPairing]s are not deleted, they may still be referenced by other roles.
func (l *LicenseDB) RemoveModelEndpointPairingsFromRole(ctx context.Context, roleID uint, pairings []ModelEndpointPairing) error {
	err := l.db.WithContext(ctx).Model(&Role{ID: roleID}).Association("ModelEndpointPairings").Delete(pairings) //nolint:exhaustruct
	if err != nil {
		return fmt.Errorf("removing associations for role %d: %w", roleID, err)
	}
	return nil
}
