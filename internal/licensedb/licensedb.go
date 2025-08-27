// Package licensedb handles interactions with Continuum's license database.
package licensedb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"cloud.google.com/go/cloudsqlconn"
	"github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	// DialectMySQL represents the MySQL dialect.
	DialectMySQL Dialect = "mysql"
	// DialectSQLite represents the SQLite dialect.
	DialectSQLite Dialect = "sqlite"
)

// LicenseDB is a handle to the Continuum license database.
type LicenseDB struct {
	db *gorm.DB
}

// Dialect specifies the SQL dialect to use with GORM.
type Dialect string

// New creates a new LicenseDB handle.
func New(ctx context.Context, userName, databaseName, sqlConnectionString string, log *slog.Logger) (*LicenseDB, error) {
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

	// Open connection with GORM
	db, err := gorm.Open(gormmysql.Open(dbURI), &gorm.Config{ //nolint:exhaustruct
		Logger: newGORMLogger(log),
	})
	if err != nil {
		return nil, fmt.Errorf("opening sql database: %w", err)
	}

	// Configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("getting underlying sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(5)
	sqlDB.SetConnMaxLifetime(time.Minute)

	return &LicenseDB{db: db}, nil
}

// NewFromSQLDatabase creates a LicenseDB handle using the given SQL DB handle.
// The dialect parameter specifies which SQL dialect to use (mysql or sqlite).
func NewFromSQLDatabase(dialect Dialect, db *sql.DB, log *slog.Logger) (*LicenseDB, error) {
	var gormDB *gorm.DB
	var err error

	switch dialect {
	case DialectMySQL:
		gormDB, err = gorm.Open(gormmysql.New(gormmysql.Config{ //nolint:exhaustruct
			Conn: db,
		}), &gorm.Config{ //nolint:exhaustruct
			Logger: newGORMLogger(log),
		})

	case DialectSQLite:
		gormDB, err = gorm.Open(gormsqlite.New(gormsqlite.Config{ //nolint:exhaustruct
			Conn: db,
		}), &gorm.Config{ //nolint:exhaustruct
			Logger: newGORMLogger(log),
		})

	default:
		return nil, fmt.Errorf("unsupported SQL dialect: %s", dialect)
	}

	if err != nil {
		return nil, fmt.Errorf("initializing gorm: %w", err)
	}

	return &LicenseDB{db: gormDB}, nil
}

// Close closes the connection to the database.
func (l *LicenseDB) Close() error {
	sqlDB, err := l.db.DB()
	if err != nil {
		return fmt.Errorf("getting underlying sql.DB: %w", err)
	}
	return sqlDB.Close()
}

// GetGormDB returns the GORM DB handle. This is meant for specialized queries.
func (l *LicenseDB) GetGormDB() *gorm.DB {
	return l.db
}

func newGORMLogger(log *slog.Logger) logger.Interface {
	return &gormLogger{
		log:           log,
		level:         logger.Warn,
		slowThreshold: 400 * time.Millisecond, // GORM default is 200 ms
	}
}

type gormLogger struct {
	log           *slog.Logger
	level         logger.LogLevel
	slowThreshold time.Duration
}

func (l *gormLogger) LogMode(level logger.LogLevel) logger.Interface {
	l.level = level
	return l
}

func (l *gormLogger) Info(ctx context.Context, msg string, args ...any) {
	if l.level >= logger.Info {
		l.log.InfoContext(ctx, l.replaceWhitespaceStr(msg), args...)
	}
}

func (l *gormLogger) Warn(ctx context.Context, msg string, args ...any) {
	if l.level >= logger.Warn {
		l.log.WarnContext(ctx, l.replaceWhitespaceStr(msg), args...)
	}
}

func (l *gormLogger) Error(ctx context.Context, msg string, args ...any) {
	if l.level >= logger.Error {
		l.log.ErrorContext(ctx, l.replaceWhitespaceStr(msg), args...)
	}
}

func (l *gormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.level <= logger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()
	sql = l.replaceWhitespaceStr(sql)
	switch {
	case err != nil && l.level >= logger.Error && !errors.Is(err, logger.ErrRecordNotFound):
		l.log.ErrorContext(ctx, "SQL Error", "duration", float64(elapsed.Nanoseconds())/1e6, "rows", rows, "sqlQuery", sql, "error", err)
	case elapsed > l.slowThreshold && l.slowThreshold != 0 && l.level >= logger.Warn:
		l.log.WarnContext(ctx, "Slow SQL", "duration", float64(elapsed.Nanoseconds())/1e6, "rows", rows, "sqlQuery", sql)
	case l.level == logger.Info:
		l.log.InfoContext(ctx, "SQL Query", "duration", float64(elapsed.Nanoseconds())/1e6, "rows", rows, "sqlQuery", sql)
	}
}

func (l *gormLogger) replaceWhitespaceStr(msg string) string {
	return strings.ReplaceAll(strings.ReplaceAll(msg, "\n", " "), "\t", " ")
}
