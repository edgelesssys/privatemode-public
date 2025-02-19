package licensedb

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	// TokenUsageTable is the table name of the table holding the token usage of each license.
	TokenUsageTable                  = "token_usage"
	tokenUsageLicenseKeyColumn       = "license_key"
	tokenUsagePromptTokensColumn     = "prompt_tokens"
	tokenUsageCompletionTokensColumn = "completion_tokens"
	tokeUsageTimestampColumn         = "timestamp"
)

var allTokenUsageColumns = []string{
	tokenUsageLicenseKeyColumn, tokenUsagePromptTokensColumn,
	tokenUsageCompletionTokensColumn, tokeUsageTimestampColumn,
}

// GetUsageEntries returns all entries from table "token_usage".
func (l *LicenseDB) GetUsageEntries(ctx context.Context) ([]UsageEntry, error) {
	stmt, err := l.db.PrepareContext(ctx, fmt.Sprintf("SELECT %s FROM %s", strings.Join(allTokenUsageColumns, ", "), TokenUsageTable))
	if err != nil {
		return nil, fmt.Errorf("preparing query: %w", err)
	}
	defer stmt.Close()
	rows, err := stmt.QueryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("executing query: %w", err)
	}
	defer rows.Close()

	var entries []UsageEntry
	for rows.Next() {
		var entry UsageEntry
		if err := rows.Scan(
			&entry.LicenseKey, &entry.PromptTokens, &entry.CompletionTokens, &entry.Timestamp,
		); err != nil {
			return nil, fmt.Errorf("reading row: %w", err)
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// GetUsageEntriesByLicenseKey fetches usage information for a specific license key.
func (l *LicenseDB) GetUsageEntriesByLicenseKey(ctx context.Context, licenseKey string) ([]UsageEntry, error) {
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = ?", strings.Join(allTokenUsageColumns, ", "), TokenUsageTable, tokenUsageLicenseKeyColumn)

	stmt, err := l.db.PrepareContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("preparing query: %w", err)
	}
	defer stmt.Close()

	rows, err := stmt.QueryContext(ctx, licenseKey)
	if err != nil {
		return nil, fmt.Errorf("executing query: %w", err)
	}
	defer rows.Close()

	var entries []UsageEntry
	for rows.Next() {
		var entry UsageEntry
		if err := rows.Scan(&entry.LicenseKey, &entry.PromptTokens, &entry.CompletionTokens, &entry.Timestamp); err != nil {
			return nil, fmt.Errorf("reading row: %w", err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// InsertUsageEntries inserts the given usage entries into the database.
func (l *LicenseDB) InsertUsageEntries(ctx context.Context, entries []UsageEntry) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}

	query := fmt.Sprintf("INSERT INTO %s (license_key, prompt_tokens, completion_tokens) VALUES (?, ?, ?)", TokenUsageTable)
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("preparing query: %w", err)
	}
	defer stmt.Close()

	for _, entry := range entries {
		if _, err := stmt.ExecContext(ctx, entry.LicenseKey, entry.PromptTokens, entry.CompletionTokens); err != nil {
			return fmt.Errorf("executing insert: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

// UsageEntry represents a single usage entry in the token_usage table of the Continuum license database.
type UsageEntry struct {
	LicenseKey       string
	PromptTokens     int64
	CompletionTokens int64
	Timestamp        time.Time
}

// TableHeader returns the column names of the table as a tab-separated string.
func (e UsageEntry) TableHeader() string {
	return "LicenseKey\tPromptTokens\tCompletionTokens\tTimestamp"
}

// String is used to print usage entries as rows in a table format.
func (e UsageEntry) String() string {
	return fmt.Sprintf("%s\t%d\t%d\t%s",
		e.LicenseKey, e.PromptTokens, e.CompletionTokens, e.Timestamp.Format("02.01.2006 15:04:05"),
	)
}

// Slice returns the entry as a slice of strings.
func (e UsageEntry) Slice() []string {
	return []string{
		e.LicenseKey, fmt.Sprintf("%d", e.PromptTokens), fmt.Sprintf("%d", e.CompletionTokens), e.Timestamp.Format("02.01.2006 15:04:05"),
	}
}
