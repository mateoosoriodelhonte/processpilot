package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sync"
	"time"

	"github.com/mateoosoriodelhonte/processpilot/internal/analysis"
	"github.com/mateoosoriodelhonte/processpilot/internal/protocol"
	_ "modernc.org/sqlite"
)

const (
	DefaultRetention              = 7 * 24 * time.Hour
	MinimumRetention              = time.Hour
	MaximumRetention              = 30 * 24 * time.Hour
	HistoryInterval               = time.Minute
	MaximumHistoricalApplications = 100
)

type Store struct {
	db             *sql.DB
	retention      time.Duration
	mutex          sync.Mutex
	lastRecordedMS int64
	lastCleanupMS  int64
}

func Open(path string, retention time.Duration) (*Store, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, errors.New("database path must be absolute")
	}
	if retention < MinimumRetention || retention > MaximumRetention {
		return nil, fmt.Errorf("retention must be between %s and %s", MinimumRetention, MaximumRetention)
	}

	db, err := sql.Open("sqlite", filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := configure(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	var lastRecordedMS int64
	if err := db.QueryRow("SELECT COALESCE(MAX(timestamp_ms), 0) FROM system_samples").Scan(&lastRecordedMS); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("read latest telemetry timestamp: %w", err)
	}
	return &Store{db: db, retention: retention, lastRecordedMS: lastRecordedMS}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Record(ctx context.Context, snapshot protocol.Snapshot, result analysis.Result) error {
	if snapshot.TimestampUnixMS > math.MaxInt64 {
		return errors.New("snapshot timestamp exceeds SQLite integer range")
	}
	timestamp := int64(snapshot.TimestampUnixMS)
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.lastRecordedMS > 0 && timestamp < s.lastRecordedMS+HistoryInterval.Milliseconds() {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin telemetry transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO system_samples (
			timestamp_ms, sequence, total_memory_bytes, used_memory_bytes, available_memory_bytes,
			total_swap_bytes, used_swap_bytes, cpu_percent, load_average_1, load_average_5,
			load_average_15, logical_cpu_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		timestamp, snapshot.Sequence, snapshot.System.TotalMemoryBytes, snapshot.System.UsedMemoryBytes,
		snapshot.System.AvailableMemoryBytes, snapshot.System.TotalSwapBytes, snapshot.System.UsedSwapBytes,
		snapshot.System.CPUPercent, snapshot.System.LoadAverage1, snapshot.System.LoadAverage5,
		snapshot.System.LoadAverage15, snapshot.System.LogicalCPUCount,
	)
	if err != nil {
		return fmt.Errorf("insert system sample: %w", err)
	}

	applications := result.Applications
	if len(applications) > MaximumHistoricalApplications {
		applications = applications[:MaximumHistoricalApplications]
	}
	for _, application := range applications {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO application_samples (
				timestamp_ms, app_key, application, category, stopping_risk, process_count, cpu_percent, memory_bytes
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			timestamp, application.Key, application.Name, application.Category, application.Risk,
			application.ProcessCount, application.CPUPercent, application.MemoryBytes,
		)
		if err != nil {
			return fmt.Errorf("insert application sample: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit telemetry transaction: %w", err)
	}
	s.lastRecordedMS = timestamp
	return nil
}

func (s *Store) ApplicationHistory(ctx context.Context, application string, since time.Time, limit int) ([]analysis.HistoryPoint, error) {
	if limit < 1 || limit > 10_000 {
		return nil, errors.New("history limit must be between 1 and 10000")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT timestamp_ms, application, memory_bytes, cpu_percent
		FROM (
			SELECT timestamp_ms, application, memory_bytes, cpu_percent
			FROM application_samples
			WHERE application = ? AND timestamp_ms >= ?
			ORDER BY timestamp_ms DESC
			LIMIT ?
		)
		ORDER BY timestamp_ms ASC`, application, since.UnixMilli(), limit)
	if err != nil {
		return nil, fmt.Errorf("query application history: %w", err)
	}
	defer rows.Close()

	var history []analysis.HistoryPoint
	for rows.Next() {
		var timestampMS int64
		var point analysis.HistoryPoint
		if err := rows.Scan(&timestampMS, &point.Application, &point.MemoryBytes, &point.CPUPercent); err != nil {
			return nil, fmt.Errorf("scan application history: %w", err)
		}
		point.Timestamp = time.UnixMilli(timestampMS).UTC()
		history = append(history, point)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate application history: %w", err)
	}
	return history, nil
}

func (s *Store) AllApplicationHistory(ctx context.Context, since time.Time, limit int) ([]analysis.HistoryPoint, error) {
	if limit < 1 || limit > 100_000 {
		return nil, errors.New("history limit must be between 1 and 100000")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT timestamp_ms, application, memory_bytes, cpu_percent
		FROM (
			SELECT timestamp_ms, application, memory_bytes, cpu_percent
			FROM application_samples
			WHERE timestamp_ms >= ?
			ORDER BY timestamp_ms DESC, application DESC
			LIMIT ?
		)
		ORDER BY timestamp_ms ASC, application ASC`, since.UnixMilli(), limit)
	if err != nil {
		return nil, fmt.Errorf("query history: %w", err)
	}
	defer rows.Close()

	var history []analysis.HistoryPoint
	for rows.Next() {
		var timestampMS int64
		var point analysis.HistoryPoint
		if err := rows.Scan(&timestampMS, &point.Application, &point.MemoryBytes, &point.CPUPercent); err != nil {
			return nil, fmt.Errorf("scan history: %w", err)
		}
		point.Timestamp = time.UnixMilli(timestampMS).UTC()
		history = append(history, point)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate history: %w", err)
	}
	return history, nil
}

func (s *Store) Cleanup(ctx context.Context, now time.Time) (int64, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	nowMS := now.UnixMilli()
	if s.lastCleanupMS > 0 && nowMS < s.lastCleanupMS+int64((10*time.Minute)/time.Millisecond) {
		return 0, nil
	}
	cutoff := now.Add(-s.retention).UnixMilli()
	result, err := s.db.ExecContext(ctx, "DELETE FROM system_samples WHERE timestamp_ms < ?", cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete expired telemetry: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count expired telemetry: %w", err)
	}
	s.lastCleanupMS = nowMS
	return deleted, nil
}

func configure(db *sql.DB) error {
	for _, statement := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := db.Exec(statement); err != nil {
			return fmt.Errorf("configure sqlite: %w", err)
		}
	}
	return nil
}

type migration struct {
	version int
	sql     string
}

var migrations = []migration{
	{
		version: 1,
		sql: `
		CREATE TABLE system_samples (
			timestamp_ms INTEGER PRIMARY KEY,
			sequence INTEGER NOT NULL,
			total_memory_bytes INTEGER NOT NULL,
			used_memory_bytes INTEGER NOT NULL,
			available_memory_bytes INTEGER NOT NULL,
			total_swap_bytes INTEGER NOT NULL,
			used_swap_bytes INTEGER NOT NULL,
			cpu_percent REAL NOT NULL,
			load_average_1 REAL NOT NULL,
			load_average_5 REAL NOT NULL,
			load_average_15 REAL NOT NULL,
			logical_cpu_count INTEGER NOT NULL
		);
		CREATE TABLE application_samples (
			timestamp_ms INTEGER NOT NULL REFERENCES system_samples(timestamp_ms) ON DELETE CASCADE,
			application TEXT NOT NULL,
			category TEXT NOT NULL,
			stopping_risk TEXT NOT NULL,
			process_count INTEGER NOT NULL,
			cpu_percent REAL NOT NULL,
			memory_bytes INTEGER NOT NULL,
			PRIMARY KEY (timestamp_ms, application)
		);
		CREATE TABLE process_samples (
			timestamp_ms INTEGER NOT NULL REFERENCES system_samples(timestamp_ms) ON DELETE CASCADE,
			pid INTEGER NOT NULL,
			start_time_unix_seconds INTEGER NOT NULL,
			parent_pid INTEGER,
			name TEXT NOT NULL,
			executable TEXT,
			status TEXT NOT NULL,
			application TEXT NOT NULL,
			category TEXT NOT NULL,
			stopping_risk TEXT NOT NULL,
			cpu_percent REAL NOT NULL,
			memory_bytes INTEGER NOT NULL,
			PRIMARY KEY (timestamp_ms, pid, start_time_unix_seconds)
		);
		CREATE INDEX application_samples_lookup ON application_samples(application, timestamp_ms);
		CREATE INDEX process_samples_identity ON process_samples(name, start_time_unix_seconds, timestamp_ms);`,
	},
	{
		version: 2,
		sql: `
		CREATE TABLE application_samples_v2 (
			timestamp_ms INTEGER NOT NULL REFERENCES system_samples(timestamp_ms) ON DELETE CASCADE,
			app_key TEXT NOT NULL,
			application TEXT NOT NULL,
			category TEXT NOT NULL,
			stopping_risk TEXT NOT NULL,
			process_count INTEGER NOT NULL,
			cpu_percent REAL NOT NULL,
			memory_bytes INTEGER NOT NULL,
			PRIMARY KEY (timestamp_ms, app_key)
		);
		INSERT INTO application_samples_v2 (
			timestamp_ms, app_key, application, category, stopping_risk, process_count, cpu_percent, memory_bytes
		)
		SELECT timestamp_ms, 'legacy:' || application, application, category, stopping_risk, process_count, cpu_percent, memory_bytes
		FROM application_samples;
		DROP TABLE application_samples;
		ALTER TABLE application_samples_v2 RENAME TO application_samples;
		CREATE INDEX application_samples_lookup ON application_samples(application, timestamp_ms);`,
	},
	{
		version: 3,
		sql:     `DROP TABLE IF EXISTS process_samples;`,
	},
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at_ms INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	var current int
	if err := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&current); err != nil {
		return fmt.Errorf("read migration version: %w", err)
	}
	for _, migration := range migrations {
		if migration.version <= current {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", migration.version, err)
		}
		if _, err := tx.Exec(migration.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", migration.version, err)
		}
		if _, err := tx.Exec("INSERT INTO schema_migrations (version, applied_at_ms) VALUES (?, ?)", migration.version, time.Now().UTC().UnixMilli()); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", migration.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", migration.version, err)
		}
	}
	return nil
}
