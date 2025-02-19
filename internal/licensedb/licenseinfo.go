package licensedb

import (
	"context"
	"fmt"
	"strconv"
	"strings"
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
	LicenseInfoTable                           = "license_info"
	licenseInfoLicenseKeyColumn                = "license_key"
	licenseInfoOrganizationColumn              = "organization"
	licenseInfoIssueDateColumn                 = "issue_date"
	licenseInfoExpirationDateColumn            = "expiration_date"
	licenseInfoUsageLimitColumn                = "usage_limit"
	licenseInfoPromptTokensPerMinuteColumn     = "prompt_tokens_per_minute"
	licenseInfoCompletionTokensPerMinuteColumn = "completion_tokens_per_minute"
	licenseInfoRequestsPerMinuteColumn         = "requests_per_minute"
	licenseInfoStripeCustomerIDColumn          = "stripe_customer_id"
	licenseInfoTypeColumn                      = "type"
	licenseInfoCommentColumn                   = "comment"
)

var allLicenseInfoColumns = []string{
	licenseInfoLicenseKeyColumn, licenseInfoOrganizationColumn, licenseInfoIssueDateColumn,
	licenseInfoExpirationDateColumn, licenseInfoUsageLimitColumn, licenseInfoPromptTokensPerMinuteColumn,
	licenseInfoCompletionTokensPerMinuteColumn, licenseInfoRequestsPerMinuteColumn, licenseInfoStripeCustomerIDColumn,
	licenseInfoTypeColumn, licenseInfoCommentColumn,
}

// GetLicenseEntries returns all license entries from the database.
// The returned list of entries is unsorted.
func (l *LicenseDB) GetLicenseEntries(ctx context.Context) ([]LicenseEntry, error) {
	stmt, err := l.db.PrepareContext(ctx, fmt.Sprintf("SELECT %s FROM %s", strings.Join(allLicenseInfoColumns, ", "), LicenseInfoTable))
	if err != nil {
		return nil, fmt.Errorf("preparing query: %w", err)
	}
	defer stmt.Close()
	rows, err := stmt.QueryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("executing query: %w", err)
	}
	defer rows.Close()

	var entries []LicenseEntry
	for rows.Next() {
		var entry LicenseEntry
		if err := rows.Scan(
			&entry.LicenseKey, &entry.Organization,
			&entry.IssueDate, &entry.ExpirationDate,
			&entry.UsageLimit, &entry.PromptTokensPerMinute,
			&entry.CompletionTokensPerMinute, &entry.RequestsPerMinute,
			&entry.StripeCustomerID, &entry.Type, &entry.Comment,
		); err != nil {
			return nil, fmt.Errorf("reading row: %w", err)
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// GetLicenseEntryByLicenseKey returns the license entry for the given license key.
func (l *LicenseDB) GetLicenseEntryByLicenseKey(ctx context.Context, licenseKey string) (LicenseEntry, error) {
	var entry LicenseEntry
	stmt, err := l.db.PrepareContext(ctx, fmt.Sprintf("SELECT %s FROM %s WHERE license_key = ?", strings.Join(allLicenseInfoColumns, ", "), LicenseInfoTable))
	if err != nil {
		return entry, fmt.Errorf("preparing query: %w", err)
	}
	defer stmt.Close()
	row := stmt.QueryRowContext(ctx, licenseKey)
	if err := row.Scan(
		&entry.LicenseKey, &entry.Organization,
		&entry.IssueDate, &entry.ExpirationDate,
		&entry.UsageLimit, &entry.PromptTokensPerMinute,
		&entry.CompletionTokensPerMinute, &entry.RequestsPerMinute,
		&entry.StripeCustomerID, &entry.Type, &entry.Comment,
	); err != nil {
		return entry, fmt.Errorf("scanning row: %w", err)
	}
	return entry, nil
}

// InsertLicenseEntry inserts a new license entry into the database.
func (l *LicenseDB) InsertLicenseEntry(ctx context.Context, entry LicenseEntry) error {
	query := fmt.Sprintf(
		"INSERT INTO %s (license_key, organization, issue_date, expiration_date, usage_limit, "+
			"prompt_tokens_per_minute, completion_tokens_per_minute, requests_per_minute, stripe_customer_id, type, comment) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		LicenseInfoTable,
	)

	stmt, err := l.db.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("preparing insert statement: %w", err)
	}
	defer stmt.Close()
	if _, err := stmt.ExecContext(
		ctx,
		entry.LicenseKey, entry.Organization,
		entry.IssueDate, entry.ExpirationDate,
		entry.UsageLimit, entry.PromptTokensPerMinute,
		entry.CompletionTokensPerMinute, entry.RequestsPerMinute,
		entry.StripeCustomerID, entry.Type, entry.Comment,
	); err != nil {
		return fmt.Errorf("executing insert: %w", err)
	}
	return nil
}

// UpdateLicenseEntry updates an entry in the database.
func (l *LicenseDB) UpdateLicenseEntry(ctx context.Context, entry UpdateLicenseEntry) error {
	var updateArgs []any
	var updateQuery []string
	if entry.Organization != nil {
		updateQuery = append(updateQuery, "organization = ?")
		updateArgs = append(updateArgs, entry.Organization)
	}
	if entry.ExpirationDate != nil {
		updateQuery = append(updateQuery, "expiration_date = ?")
		updateArgs = append(updateArgs, entry.ExpirationDate)
	}
	if entry.UsageLimit != nil {
		updateQuery = append(updateQuery, "usage_limit = ?")
		updateArgs = append(updateArgs, entry.UsageLimit)
	}
	if entry.PromptTokensPerMinute != nil {
		updateQuery = append(updateQuery, "prompt_tokens_per_minute = ?")
		updateArgs = append(updateArgs, entry.PromptTokensPerMinute)
	}
	if entry.CompletionTokensPerMinute != nil {
		updateQuery = append(updateQuery, "completion_tokens_per_minute = ?")
		updateArgs = append(updateArgs, entry.CompletionTokensPerMinute)
	}
	if entry.RequestsPerMinute != nil {
		updateQuery = append(updateQuery, "requests_per_minute = ?")
		updateArgs = append(updateArgs, entry.RequestsPerMinute)
	}
	if entry.StripeCustomerID != nil {
		updateQuery = append(updateQuery, "stripe_customer_id = ?")
		updateArgs = append(updateArgs, *entry.StripeCustomerID)
	}
	if entry.Type != nil {
		updateQuery = append(updateQuery, "type = ?")
		updateArgs = append(updateArgs, *entry.Type)
	}
	if entry.Comment != nil {
		updateQuery = append(updateQuery, "comment = ?")
		updateArgs = append(updateArgs, entry.Comment)
	}
	if len(updateQuery) == 0 {
		return nil
	}

	stmt, err := l.db.PrepareContext(ctx, fmt.Sprintf("UPDATE %s SET %s WHERE license_key = ?", LicenseInfoTable, strings.Join(updateQuery, ", ")))
	if err != nil {
		return fmt.Errorf("preparing update statement: %w", err)
	}
	defer stmt.Close()
	if _, err := stmt.ExecContext(ctx, append(updateArgs, entry.LicenseKey)...); err != nil {
		return fmt.Errorf("executing update: %w", err)
	}
	return nil
}

// LicenseEntry is an entry in the license database.
type LicenseEntry struct {
	LicenseKey                string    `json:"license_key"`
	Organization              string    `json:"organization"`
	IssueDate                 time.Time `json:"issue_date"`
	ExpirationDate            time.Time `json:"expiration_date"`
	UsageLimit                int64     `json:"usage_limit"`
	PromptTokensPerMinute     int64     `json:"prompt_tokens_per_minute"`
	CompletionTokensPerMinute int64     `json:"completion_tokens_per_minute"`
	RequestsPerMinute         int64     `json:"requests_per_minute"`
	StripeCustomerID          *string   `json:"stripe_customer_id"`
	Type                      string    `json:"type"`
	Comment                   string    `json:"comment"`
}

// TableHeader returns the column names of the table as a tab-separated string.
func (e LicenseEntry) TableHeader() string {
	return "LicenseKey\tOrganization\tIssueDate\tExpirationDate\tUsageLimit\tPromptTokens/Minute\tCompletionTokens/Minute\tRequests/Minute\tStripeCustomerID\tType\tComment"
}

// String returns the entry as a tab-separated string.
func (e LicenseEntry) String() string {
	stripeCustomerID := ""
	if e.StripeCustomerID != nil {
		stripeCustomerID = *e.StripeCustomerID
	}
	return fmt.Sprintf("%s\t%s\t%s\t%s\t%d\t%d\t%d\t%d\t%s\t%s\t%s",
		e.LicenseKey, e.Organization,
		e.IssueDate.Format("02.01.2006"), e.ExpirationDate.Format("02.01.2006"),
		e.UsageLimit, e.PromptTokensPerMinute, e.CompletionTokensPerMinute,
		e.RequestsPerMinute, e.Type, stripeCustomerID,
		e.Comment,
	)
}

// Slice returns the entry as a slice of strings.
func (e LicenseEntry) Slice() []string {
	stripeCustomerID := ""
	if e.StripeCustomerID != nil {
		stripeCustomerID = *e.StripeCustomerID
	}

	return []string{
		e.LicenseKey, e.Organization,
		e.IssueDate.Format("02.01.2006"), e.ExpirationDate.Format("02.01.2006"),
		strconv.FormatInt(e.UsageLimit, 10), strconv.FormatInt(e.PromptTokensPerMinute, 10),
		strconv.FormatInt(e.CompletionTokensPerMinute, 10), strconv.FormatInt(e.RequestsPerMinute, 10),
		stripeCustomerID, e.Type, e.Comment,
	}
}

// UpdateLicenseEntry is used to update an entry in the license database.
// nil fields are ignored.
type UpdateLicenseEntry struct {
	LicenseKey                string     `json:"license_key"`
	Organization              *string    `json:"organization,omitempty"`
	ExpirationDate            *time.Time `json:"expiration_date,omitempty"`
	UsageLimit                *int64     `json:"usage_limit,omitempty"`
	PromptTokensPerMinute     *int64     `json:"prompt_tokens_per_minute,omitempty"`
	CompletionTokensPerMinute *int64     `json:"completion_tokens_per_minute,omitempty"`
	RequestsPerMinute         *int64     `json:"requests_per_minute,omitempty"`
	StripeCustomerID          *string    `json:"stripe_customer_id,omitempty"`
	Type                      *string    `json:"type,omitempty"`
	Comment                   *string    `json:"comment,omitempty"`
}
