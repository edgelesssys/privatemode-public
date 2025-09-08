package licensedb

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
)

const (
	// TokenUsageTable is the table name of the table holding the token usage of each license.
	TokenUsageTable = "token_usage"
	// GroupByAPIKey groups usage data by API key (license key).
	GroupByAPIKey UsageGroupBy = "license_key"
	// GroupByModel groups usage data by model name.
	GroupByModel UsageGroupBy = "model_name"
)

// UsageGroupBy defines the grouping options for usage data.
type UsageGroupBy string

// DailyUsage represents the usage data grouped by day and group key.
type DailyUsage struct {
	GroupKey           string `gorm:"column:group_key"`
	Day                int    `gorm:"column:day"`
	PromptTokens       int    `gorm:"column:prompt_tokens"`
	CachedPromptTokens int    `gorm:"column:cached_prompt_tokens"`
	CompletionTokens   int    `gorm:"column:completion_tokens"`
	FileSizeMB         int    `gorm:"column:file_size_mb"`
	TotalTokens        int    `gorm:"column:total_tokens"`
}

// GetUsageEntries returns all entries from table "token_usage".
func (l *LicenseDB) GetUsageEntries(ctx context.Context) ([]UsageEntry, error) {
	var entries []UsageEntry
	result := l.db.WithContext(ctx).Find(&entries)
	if result.Error != nil {
		return nil, fmt.Errorf("querying usage entries: %w", result.Error)
	}
	return entries, nil
}

// GetUsageEntriesByLicenseKey fetches usage information for a specific license key.
func (l *LicenseDB) GetUsageEntriesByLicenseKey(ctx context.Context, licenseKey string) ([]UsageEntry, error) {
	var entries []UsageEntry
	result := l.db.WithContext(ctx).Where("license_key = ?", licenseKey).Find(&entries)
	if result.Error != nil {
		return nil, fmt.Errorf("querying usage entries by license key: %w", result.Error)
	}
	return entries, nil
}

// GetUsageEntriesByLicenseKeyInPeriod fetches usage information for a specific license key within a given time period.
func (l *LicenseDB) GetUsageEntriesByLicenseKeyInPeriod(
	ctx context.Context, licenseKey, modelName, apiEndpoint string, startDate, endDate time.Time,
) ([]UsageEntry, error) {
	var entries []UsageEntry
	result := l.db.WithContext(ctx).
		Where(
			"license_key = ? AND model_name = ? AND api_endpoint = ? AND timestamp BETWEEN ? AND ?",
			licenseKey, modelName, apiEndpoint, startDate, endDate,
		).
		Find(&entries)
	if result.Error != nil {
		return nil, fmt.Errorf("querying usage entries by license key in period: %w", result.Error)
	}
	return entries, nil
}

// GetTotalUsageInPeriod returns the total usage for all license keys between the given dates.
func (l *LicenseDB) GetTotalUsageInPeriod(ctx context.Context, startDate, endDate time.Time) ([]UsageEntry, error) {
	var entries []UsageEntry

	// Using raw SQL with GORM for the aggregation query
	result := l.db.WithContext(ctx).Raw(`
		SELECT
			license_key,
			SUM(prompt_tokens) as prompt_tokens,
			SUM(completion_tokens) as completion_tokens,
			SUM(cached_prompt_tokens) as cached_prompt_tokens,
			SUM(file_size_mb) as file_size_mb
		FROM token_usage
		WHERE timestamp BETWEEN ? AND ?
		GROUP BY license_key`,
		startDate, endDate,
	).Scan(&entries)

	if result.Error != nil {
		return nil, fmt.Errorf("querying total usage in period: %w", result.Error)
	}

	// Set the timestamp to the end date for all entries
	for i := range entries {
		entries[i].Timestamp = endDate
	}

	return entries, nil
}

// InsertUsageEntries inserts the given usage entries into the database.
func (l *LicenseDB) InsertUsageEntries(ctx context.Context, entries []UsageEntry) (err error) {
	// Start a transaction
	tx := l.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return fmt.Errorf("beginning transaction: %w", tx.Error)
	}

	// Ensure rollback on error
	defer func() {
		if err != nil {
			rErr := tx.Rollback().Error
			if rErr != nil {
				err = errors.Join(err, fmt.Errorf("rolling back transaction: %w", rErr))
			}
		}
	}()

	// Insert each entry
	for _, entry := range entries {
		result := tx.Create(&entry)
		if result.Error != nil {
			err = fmt.Errorf("inserting usage entry: %w", result.Error)
			return
		}
	}

	// Commit the transaction
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

// UsageEntry represents a single usage entry in the token_usage table of the Continuum license database.
type UsageEntry struct {
	ID                 uint          `gorm:"column:id;primaryKey;autoIncrement;not null"`
	LicenseKey         string        `gorm:"column:license_key;type:varchar(36)"`
	ModelName          string        `gorm:"column:model_name;type:varchar(256);not null;default:'unknown'"`
	APIEndpoint        string        `gorm:"column:api_endpoint;type:varchar(256);not null;default:'unknown'"`
	PromptTokens       int64         `gorm:"column:prompt_tokens;type:BIGINT;not null"`
	CachedPromptTokens int64         `gorm:"column:cached_prompt_tokens;type:BIGINT;not null;default:0"`
	CompletionTokens   int64         `gorm:"column:completion_tokens;type:BIGINT;not null"`
	FileSizeMB         int64         `gorm:"column:file_size_mb;type:BIGINT;not null;default:0"`
	Timestamp          time.Time     `gorm:"column:timestamp;type:TIMESTAMP;default:CURRENT_TIMESTAMP"`
	OrganizationID     *uint         `gorm:"column:organization_id"` // TODO(daniel-weisse): make not nullable, after migration
	Organization       *Organization `gorm:"foreignKey:OrganizationID"`
}

// Add sums up the usage of two entries and returns the result.
// Metadata (LicenseKey, Timestamp, etc.) of the first entry is preserved.
func (e UsageEntry) Add(other UsageEntry) UsageEntry {
	e.ID = 0
	e.PromptTokens += other.PromptTokens
	e.CachedPromptTokens += other.CachedPromptTokens
	e.CompletionTokens += other.CompletionTokens
	e.FileSizeMB += other.FileSizeMB
	return e
}

// TableName overrides the table name.
func (UsageEntry) TableName() string {
	return TokenUsageTable
}

// TableHeader returns the column names of the table as a tab-separated string.
func (e UsageEntry) TableHeader() string {
	return "ID\tLicenseKey\tModelName\tAPIEndpoint\tPromptTokens\tCompletionTokens\tFileSizeMB\tTimestamp"
}

// String is used to print usage entries as rows in a table format.
func (e UsageEntry) String() string {
	return fmt.Sprintf("%d\t%s\t%s\t%s\t%d\t%d\t%d\t%s",
		e.ID, e.LicenseKey, e.ModelName, e.APIEndpoint, e.PromptTokens, e.CompletionTokens, e.FileSizeMB, e.Timestamp.Format("02.01.2006 15:04:05"),
	)
}

// Slice returns the entry as a slice of strings.
func (e UsageEntry) Slice() []string {
	return []string{
		strconv.Itoa(int(e.ID)), e.LicenseKey, e.ModelName, e.APIEndpoint,
		strconv.Itoa(int(e.PromptTokens)), strconv.Itoa(int(e.CompletionTokens)),
		strconv.Itoa(int(e.FileSizeMB)), e.Timestamp.Format("02.01.2006 15:04:05"),
	}
}

// GetUsageByMonth retrieves token usage data grouped by month for the specified time period.
// It supports grouping by API key (license key) or model name.
func (l *LicenseDB) GetUsageByMonth(ctx context.Context, orgID uint, yearMonth string, groupBy UsageGroupBy, apiEndpoints []string) ([]DailyUsage, error) {
	startDate, err := time.Parse("2006-01", yearMonth)
	if err != nil {
		return nil, fmt.Errorf("invalid month format (expected YYYY-MM): %w", err)
	}

	endDate := startDate.AddDate(0, 1, 0).Add(-time.Second)
	dayFunc := getDayFunction(l.db.Name())

	query := fmt.Sprintf(`
		SELECT
			%[1]s as group_key,
			%[2]s as day,
			SUM(prompt_tokens) as prompt_tokens,
			SUM(cached_prompt_tokens) as cached_prompt_tokens,
			SUM(completion_tokens) as completion_tokens,
			SUM(prompt_tokens + completion_tokens + cached_prompt_tokens) as total_tokens,
			SUM(file_size_mb) as file_size_mb
		FROM %[3]s
		WHERE timestamp BETWEEN ? AND ?
		AND organization_id = ?
		AND api_endpoint IN (?)
		GROUP BY %[1]s, %[2]s
	`, string(groupBy), dayFunc, TokenUsageTable)

	var aggregates []DailyUsage
	result := l.db.WithContext(ctx).Raw(query, startDate, endDate, orgID, apiEndpoints).Scan(&aggregates)
	if result.Error != nil {
		return nil, fmt.Errorf("querying aggregated usage for month %s: %w", yearMonth, result.Error)
	}

	return aggregates, nil
}

func getDayFunction(dialect string) string {
	switch dialect {
	case "sqlite":
		// SQLite uses strftime for date functions
		// Cast to integer to remove leading zeros
		return "CAST(strftime('%d', timestamp) AS INTEGER)"
	default:
		// MySQL uses DAY()
		return "DAY(timestamp)"
	}
}
