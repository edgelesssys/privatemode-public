// package licensedb handles interactions with Continuum's license database.
package licensedb

import (
	"context"
	"database/sql"
	"fmt"
	"net"

	"cloud.google.com/go/cloudsqlconn"
	"github.com/go-sql-driver/mysql"
)

// LicenseDB is a handle to the Continuum license database.
type LicenseDB struct {
	db *sql.DB
}

// New creates a new LicenseDB handle.
func New(ctx context.Context, userName, databaseName, sqlConnectionString string) (*LicenseDB, error) {
	d, err := cloudsqlconn.NewDialer(ctx, cloudsqlconn.WithIAMAuthN())
	if err != nil {
		return nil, fmt.Errorf("setting up cloudsql dialer: %w", err)
	}

	mysql.RegisterDialContext(
		"cloudsqlconn",
		func(ctx context.Context, _ string) (net.Conn, error) {
			return d.Dial(ctx, sqlConnectionString)
		},
	)

	// format: "<user>:<password>@<connector_name>(<address>:<port>)/<db_name>[?options]"
	// Since we use IAM authentication over cloudsqlconn, we don't set a password (empty), address or port.
	// parseTime ensures local timezones don't influence the values written to the database.
	dbURI := fmt.Sprintf("%s:empty@cloudsqlconn(:)/%s?parseTime=true", userName, databaseName)
	db, err := sql.Open("mysql", dbURI)
	if err != nil {
		return nil, fmt.Errorf("opening sql database: %w", err)
	}

	return &LicenseDB{db: db}, nil
}

// Close closes the connection to the database.
func (l *LicenseDB) Close() error {
	return l.db.Close()
}
