package licensedb

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// Constants for the types of licenses.
const (
	// TypeAPI is a license key issued for API access.
	TypeAPI = "api"
	// TypeApp is a license key issued for app access.
	TypeApp = "app"
)

const (
	// LicenseInfoTable is the table name of the table holding the license keys.
	LicenseInfoTable = "license_info"
)

// GetLicenseEntries returns all license entries from the database.
// The returned list of entries is unsorted.
func (l *LicenseDB) GetLicenseEntries(ctx context.Context) ([]LicenseEntry, error) {
	var entries []LicenseEntry
	result := l.db.WithContext(ctx).Preload("Organization.Role.ModelEndpointPairings.Billing").Find(&entries)
	if result.Error != nil {
		return nil, fmt.Errorf("querying license entries: %w", result.Error)
	}
	return entries, nil
}

// GetLicenseEntryByLicenseKey returns the license entry for the given license key.
func (l *LicenseDB) GetLicenseEntryByLicenseKey(ctx context.Context, licenseKey string) (LicenseEntry, error) {
	var entry LicenseEntry
	result := l.db.WithContext(ctx).Where("license_key = ?", licenseKey).Preload("Organization.Role.ModelEndpointPairings.Billing").First(&entry)
	if result.Error != nil {
		return entry, fmt.Errorf("querying license entry: %w", result.Error)
	}
	return entry, nil
}

// GetLicenseEntriesByOrgID returns all license entries for the given organization ID.
// The returned list of entries is sorted by IssueDate in descending order (latest first).
func (l *LicenseDB) GetLicenseEntriesByOrgID(ctx context.Context, orgID uint) ([]LicenseEntry, error) {
	var entries []LicenseEntry
	result := l.db.WithContext(ctx).Where("organization_id = ?", orgID).Order("issue_date DESC").Preload("Organization.Role.ModelEndpointPairings.Billing").Find(&entries)
	if result.Error != nil {
		return nil, fmt.Errorf("querying license entries by org ID: %w", result.Error)
	}
	return entries, nil
}

// DeleteLicenseKey deletes a license entry with the given license key from the database.
func (l *LicenseDB) DeleteLicenseKey(ctx context.Context, licenseKey string, orgID uint) error {
	result := l.db.WithContext(ctx).Where("license_key = ?", licenseKey).Where("organization_id = ?", orgID).Delete(&LicenseEntry{}) //nolint:exhaustruct
	if result.Error != nil {
		return fmt.Errorf("deleting license entry: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("license key not found: %s", licenseKey)
	}
	return nil
}

// DeleteLicenseKeys deletes all license entries for the given organization ID from the database.
func (l *LicenseDB) DeleteLicenseKeys(ctx context.Context, orgID uint) error {
	result := l.db.WithContext(ctx).Where("organization_id = ?", orgID).Delete(&LicenseEntry{}) //nolint:exhaustruct
	if result.Error != nil {
		return fmt.Errorf("deleting license entries: %w", result.Error)
	}
	return nil
}

// InsertLicenseEntry inserts a new license entry into the database.
func (l *LicenseDB) InsertLicenseEntry(ctx context.Context, entry LicenseEntry) error {
	result := l.db.WithContext(ctx).Create(&entry)
	if result.Error != nil {
		return fmt.Errorf("inserting license entry: %w", result.Error)
	}
	return nil
}

// OrgRateLimitUpdate contains rate limit parameters for updating an organization's licenses.
type OrgRateLimitUpdate struct {
	PromptTokensPerMinute     *int64 `json:"prompt_tokens_per_minute,omitempty"`
	CompletionTokensPerMinute *int64 `json:"completion_tokens_per_minute,omitempty"`
	RequestsPerMinute         *int64 `json:"requests_per_minute,omitempty"`
}

// UpdateLicenseEntry updates an entry in the database.
func (l *LicenseDB) UpdateLicenseEntry(ctx context.Context, entry UpdateLicenseEntry) error {
	updates := map[string]any{}

	if entry.Name != nil {
		updates["name"] = *entry.Name
	}
	if entry.ExpirationDate != nil {
		updates["expiration_date"] = *entry.ExpirationDate
	}
	if entry.PromptTokensPerMinute != nil {
		updates["prompt_tokens_per_minute"] = *entry.PromptTokensPerMinute
	}
	if entry.CompletionTokensPerMinute != nil {
		updates["completion_tokens_per_minute"] = *entry.CompletionTokensPerMinute
	}
	if entry.RequestsPerMinute != nil {
		updates["requests_per_minute"] = *entry.RequestsPerMinute
	}
	if entry.Comment != nil {
		updates["comment"] = *entry.Comment
	}
	if entry.OrganizationID != nil {
		updates["organization_id"] = *entry.OrganizationID
	}

	if len(updates) == 0 {
		return nil
	}

	result := l.db.WithContext(ctx).Model(&LicenseEntry{}).Where("license_key = ?", entry.LicenseKey).Updates(updates) //nolint:exhaustruct
	if result.Error != nil {
		return fmt.Errorf("updating license entry: %w", result.Error)
	}
	return nil
}

// LicenseEntry is an entry in the license database.
type LicenseEntry struct {
	Name                      string       `json:"name" gorm:"column:name;type:varchar(256)"` // used to separate multiple keys in the user portal.
	LicenseKey                string       `json:"license_key" gorm:"column:license_key;primaryKey;type:varchar(36)"`
	IssueDate                 time.Time    `json:"issue_date" gorm:"column:issue_date;type:DATE;not null"`
	ExpirationDate            time.Time    `json:"expiration_date" gorm:"column:expiration_date;type:DATE;not null"` // TODO(daniel-weisse): consider removing or letting users set it themselves
	PromptTokensPerMinute     *int64       `json:"prompt_tokens_per_minute,omitempty" gorm:"column:prompt_tokens_per_minute;type:BIGINT"`
	CompletionTokensPerMinute *int64       `json:"completion_tokens_per_minute,omitempty" gorm:"column:completion_tokens_per_minute;type:BIGINT"`
	FileSizeMBPerMinute       *int64       `json:"file_size_mb_per_minute" gorm:"column:file_size_mb_per_minute;type:BIGINT"`
	RequestsPerMinute         *int64       `json:"requests_per_minute,omitempty" gorm:"column:requests_per_minute;type:BIGINT"`
	MonthlyPromptTokens       *int64       `json:"monthly_prompt_tokens" gorm:"column:monthly_prompt_tokens;type:BIGINT"`
	MonthlyCompletionTokens   *int64       `json:"monthly_completion_tokens" gorm:"column:monthly_completion_tokens;type:BIGINT"`
	MonthlyFileSizeMB         *int64       `json:"monthly_file_size_mb" gorm:"column:monthly_file_size_mb;type:BIGINT"`
	OrganizationID            uint         `json:"organization_id" gorm:"column:organization_id;index"`
	Organization              Organization `json:"organization_v2" gorm:"foreignKey:OrganizationID"` // JSON field is suffixed with v2, since organization shouldn't be taken to remain compatible with v1
	Comment                   string       `json:"comment" gorm:"column:comment;type:TEXT"`
}

// TableName overrides the table name.
func (LicenseEntry) TableName() string {
	return LicenseInfoTable
}

// TableHeader returns the column names of the table as a tab-separated string.
func (e LicenseEntry) TableHeader() string {
	return "Key\tName\tOrgID\tIssued\tExpires\tPrompt/Min\tCompl/Min\tReq/Min\tStripeID\tComment"
}

// String returns the entry as a tab-separated string.
func (e LicenseEntry) String() string {
	var promptTokensPerMinute, completionTokensPerMinute, requestsPerMinute int64
	if e.PromptTokensPerMinute != nil {
		promptTokensPerMinute = *e.PromptTokensPerMinute
	}
	if e.CompletionTokensPerMinute != nil {
		completionTokensPerMinute = *e.CompletionTokensPerMinute
	}
	if e.RequestsPerMinute != nil {
		requestsPerMinute = *e.RequestsPerMinute
	}
	return fmt.Sprintf("%s\t%s\t%d\t%s\t%s\t%d\t%d\t%d\t%s",
		e.LicenseKey, e.Name, e.OrganizationID,
		e.IssueDate.Format("02.01.2006"), e.ExpirationDate.Format("02.01.2006"),
		promptTokensPerMinute, completionTokensPerMinute,
		requestsPerMinute, e.Comment,
	)
}

// Slice returns the entry as a slice of strings.
func (e LicenseEntry) Slice() []string {
	var promptTokensPerMinute, completionTokensPerMinute, requestsPerMinute string
	if e.PromptTokensPerMinute != nil {
		promptTokensPerMinute = strconv.FormatInt(*e.PromptTokensPerMinute, 10)
	}
	if e.CompletionTokensPerMinute != nil {
		completionTokensPerMinute = strconv.FormatInt(*e.CompletionTokensPerMinute, 10)
	}
	if e.RequestsPerMinute != nil {
		requestsPerMinute = strconv.FormatInt(*e.RequestsPerMinute, 10)
	}
	return []string{
		e.LicenseKey, e.Name, strconv.FormatUint(uint64(e.OrganizationID), 10),
		e.IssueDate.Format("02.01.2006"), e.ExpirationDate.Format("02.01.2006"),
		promptTokensPerMinute,
		completionTokensPerMinute, requestsPerMinute,
		e.Comment,
	}
}

// UpdateLicenseEntry is used to update an entry in the license database.
// nil fields are ignored.
type UpdateLicenseEntry struct {
	LicenseKey                string     `json:"license_key" gorm:"column:license_key;primaryKey"`
	Name                      *string    `json:"name,omitempty" gorm:"column:name"`
	ExpirationDate            *time.Time `json:"expiration_date,omitempty" gorm:"column:expiration_date"`
	PromptTokensPerMinute     *int64     `json:"prompt_tokens_per_minute,omitempty" gorm:"column:prompt_tokens_per_minute"`
	CompletionTokensPerMinute *int64     `json:"completion_tokens_per_minute,omitempty" gorm:"column:completion_tokens_per_minute"`
	RequestsPerMinute         *int64     `json:"requests_per_minute,omitempty" gorm:"column:requests_per_minute"`
	Comment                   *string    `json:"comment,omitempty" gorm:"column:comment"`
	OrganizationID            *uint      `json:"organization_id,omitempty" gorm:"column:organization_id"`
}

// TableName overrides the table name.
func (UpdateLicenseEntry) TableName() string {
	return LicenseInfoTable
}
