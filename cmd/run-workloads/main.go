package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/ajitpratap0/GoSQLX/pkg/formatter"
	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
	_ "github.com/duckdb/duckdb-go/v2"

	"go-transformer/internal/examples"
	"go-transformer/internal/sqlspec"
)

type scenario struct {
	name            string
	expectedFailure bool
	run             func(*sql.DB, examples.DataFiles, int) error
	format          string
	mode            string
}

type telemetryLogger struct {
	format string
	level  string
}

func newTelemetryLogger(format string, level string) *telemetryLogger {
	f := strings.ToLower(strings.TrimSpace(format))
	if f != "json" {
		f = "text"
	}
	l := strings.ToLower(strings.TrimSpace(level))
	if l != "debug" {
		l = "basic"
	}
	return &telemetryLogger{format: f, level: l}
}

func (t *telemetryLogger) emit(event string, fields map[string]any) {
	payload := map[string]any{
		"ts":    time.Now().UTC().Format(time.RFC3339Nano),
		"event": event,
	}
	for k, v := range fields {
		payload[k] = v
	}

	if t.format == "json" {
		enc := json.NewEncoder(os.Stdout)
		if err := enc.Encode(payload); err != nil {
			log.Printf("telemetry encode error: %v", err)
		}
		return
	}

	parts := make([]string, 0, len(payload)+1)
	parts = append(parts, event)
	for k, v := range payload {
		if k == "event" || k == "ts" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	fmt.Printf("TELEMETRY ts=%s %s\n", payload["ts"], strings.Join(parts, " "))
}

func (t *telemetryLogger) debug(event string, fields map[string]any) {
	if t.level != "debug" {
		return
	}
	t.emit(event, fields)
}

func main() {
	rows := flag.Int("rows", 1000, "number of rows to use")
	seed := flag.Int64("seed", examples.DefaultSeed, "deterministic random seed")
	preview := flag.Int("preview", 5, "rows to preview per successful scenario")
	dataDir := flag.String("data-dir", "data", "directory containing generated data files")
	telemetryFormat := flag.String("telemetry-format", "text", "telemetry output format: text|json")
	telemetryLevel := flag.String("telemetry-level", "basic", "telemetry verbosity: basic|debug")
	flag.Parse()

	telemetry := newTelemetryLogger(*telemetryFormat, *telemetryLevel)
	runStartedAt := time.Now()
	telemetry.emit("run_start", map[string]any{
		"rows":           *rows,
		"seed":           *seed,
		"preview":        *preview,
		"data_dir":       *dataDir,
		"scenario_count": 6,
	})

	files, err := examples.GeneratePeopleFiles(*rows, *seed, *dataDir)
	if err != nil {
		log.Fatal(err)
	}

	db, err := sql.Open("duckdb", "")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	scenarios := []scenario{
		{name: "csv_infer", run: runCSVInfer, format: "csv", mode: string(sqlspec.DataSourceModeInfer)},
		{name: "csv_declared", run: runCSVDeclared, format: "csv", mode: string(sqlspec.DataSourceModeDeclared)},
		{name: "csv_declared_with_check_ok", run: runCSVDeclaredWithCheckOK, format: "csv", mode: string(sqlspec.DataSourceModeDeclaredWithCheck)},
		{name: "csv_declared_with_check_mismatch", expectedFailure: true, run: runCSVDeclaredWithCheckMismatch, format: "csv", mode: string(sqlspec.DataSourceModeDeclaredWithCheck)},
		{name: "parquet_infer", run: runParquetInfer, format: "parquet", mode: string(sqlspec.DataSourceModeInfer)},
		{name: "parquet_declared_with_check_ok", run: runParquetDeclaredWithCheckOK, format: "parquet", mode: string(sqlspec.DataSourceModeDeclaredWithCheck)},
	}
	telemetry.debug("scenario_plan", map[string]any{"names": len(scenarios)})

	passed := 0
	failed := 0
	unexpectedFailures := 0

	for _, sc := range scenarios {
		scenarioStart := time.Now()
		telemetry.emit("scenario_start", map[string]any{
			"scenario":      sc.name,
			"format":        sc.format,
			"mode":          sc.mode,
			"rows_expected": files.Rows,
			"seed":          files.Seed,
		})

		err := sc.run(db, files, *preview)
		durationMS := time.Since(scenarioStart).Milliseconds()
		switch {
		case err == nil && !sc.expectedFailure:
			passed++
			fmt.Printf("PASS: %s\n", sc.name)
			telemetry.emit("scenario_end", map[string]any{
				"scenario":      sc.name,
				"format":        sc.format,
				"mode":          sc.mode,
				"status":        "pass",
				"duration_ms":   durationMS,
				"rows_expected": files.Rows,
			})
		case err != nil && sc.expectedFailure:
			passed++
			fmt.Printf("PASS (expected failure): %s -> %v\n", sc.name, err)
			telemetry.emit("scenario_end", map[string]any{
				"scenario":      sc.name,
				"format":        sc.format,
				"mode":          sc.mode,
				"status":        "expected_fail",
				"duration_ms":   durationMS,
				"rows_expected": files.Rows,
				"error":         err.Error(),
			})
		case err == nil && sc.expectedFailure:
			failed++
			unexpectedFailures++
			fmt.Printf("FAIL: %s expected failure but passed\n", sc.name)
			telemetry.emit("scenario_end", map[string]any{
				"scenario":      sc.name,
				"format":        sc.format,
				"mode":          sc.mode,
				"status":        "fail",
				"duration_ms":   durationMS,
				"rows_expected": files.Rows,
				"error":         "expected failure but scenario passed",
			})
		default:
			failed++
			unexpectedFailures++
			fmt.Printf("FAIL: %s -> %v\n", sc.name, err)
			telemetry.emit("scenario_end", map[string]any{
				"scenario":      sc.name,
				"format":        sc.format,
				"mode":          sc.mode,
				"status":        "fail",
				"duration_ms":   durationMS,
				"rows_expected": files.Rows,
				"error":         err.Error(),
			})
		}
	}

	fmt.Printf("\nSummary: passed=%d failed=%d\n", passed, failed)
	telemetry.emit("run_end", map[string]any{
		"passed":              passed,
		"failed":              failed,
		"unexpected_failures": unexpectedFailures,
		"duration_ms":         time.Since(runStartedAt).Milliseconds(),
	})
	if unexpectedFailures > 0 {
		log.Fatalf("unexpected failures: %d", unexpectedFailures)
	}
}

func runCSVInfer(db *sql.DB, files examples.DataFiles, preview int) error {
	return runDataSourceScenario(db, "people_csv_infer", sqlspec.DataSourceSpec{
		Format: sqlspec.DataSourceFormatCSV,
		Path:   files.CSVPath,
		Mode:   sqlspec.DataSourceModeInfer,
	}, files.Rows, preview)
}

func runCSVDeclared(db *sql.DB, files examples.DataFiles, preview int) error {
	return runDataSourceScenario(db, "people_csv_declared", sqlspec.DataSourceSpec{
		Format: sqlspec.DataSourceFormatCSV,
		Path:   files.CSVPath,
		Mode:   sqlspec.DataSourceModeDeclared,
		Columns: []sqlspec.ColumnSpec{
			{Name: "id", SQLType: "BIGINT"},
			{Name: "name", SQLType: "VARCHAR"},
			{Name: "age", SQLType: "BIGINT"},
			{Name: "country", SQLType: "VARCHAR"},
		},
	}, files.Rows, preview)
}

func runCSVDeclaredWithCheckOK(db *sql.DB, files examples.DataFiles, preview int) error {
	return runDataSourceScenario(db, "people_csv_check_ok", sqlspec.DataSourceSpec{
		Format: sqlspec.DataSourceFormatCSV,
		Path:   files.CSVPath,
		Mode:   sqlspec.DataSourceModeDeclaredWithCheck,
		Columns: []sqlspec.ColumnSpec{
			{Name: "id", SQLType: "BIGINT"},
			{Name: "name", SQLType: "VARCHAR"},
			{Name: "age", SQLType: "BIGINT"},
			{Name: "country", SQLType: "VARCHAR"},
		},
	}, files.Rows, preview)
}

func runCSVDeclaredWithCheckMismatch(db *sql.DB, files examples.DataFiles, preview int) error {
	_, _, err := sqlspec.DeriveDataSource("people_csv_check_bad", sqlspec.DataSourceSpec{
		Format: sqlspec.DataSourceFormatCSV,
		Path:   files.CSVPath,
		Mode:   sqlspec.DataSourceModeDeclaredWithCheck,
		Columns: []sqlspec.ColumnSpec{
			{Name: "name", SQLType: "VARCHAR"},
			{Name: "id", SQLType: "BIGINT"},
			{Name: "age", SQLType: "BIGINT"},
			{Name: "country", SQLType: "VARCHAR"},
		},
	}, db)
	return err
}

func runParquetInfer(db *sql.DB, files examples.DataFiles, preview int) error {
	return runDataSourceScenario(db, "people_parquet_infer", sqlspec.DataSourceSpec{
		Format: sqlspec.DataSourceFormatParquet,
		Path:   files.ParquetPath,
		Mode:   sqlspec.DataSourceModeInfer,
	}, files.Rows, preview)
}

func runParquetDeclaredWithCheckOK(db *sql.DB, files examples.DataFiles, preview int) error {
	return runDataSourceScenario(db, "people_parquet_check_ok", sqlspec.DataSourceSpec{
		Format: sqlspec.DataSourceFormatParquet,
		Path:   files.ParquetPath,
		Mode:   sqlspec.DataSourceModeDeclaredWithCheck,
		Columns: []sqlspec.ColumnSpec{
			{Name: "id", SQLType: "BIGINT"},
			{Name: "name", SQLType: "VARCHAR"},
			{Name: "age", SQLType: "BIGINT"},
			{Name: "country", SQLType: "VARCHAR"},
		},
	}, files.Rows, preview)
}

func runDataSourceScenario(db *sql.DB, tableName string, spec sqlspec.DataSourceSpec, expectedRows int, preview int) error {
	resolved, backfill, err := sqlspec.DeriveDataSource(tableName, spec, db)
	if err != nil {
		return err
	}

	if _, err := db.Exec("DROP TABLE IF EXISTS " + tableName); err != nil {
		return err
	}

	createStmt, err := sqlspec.BuildCreateTable(resolved.Table)
	if err != nil {
		return err
	}

	if _, err := db.Exec(formatter.FormatStatement(createStmt, ast.CompactStyle())); err != nil {
		return err
	}
	if _, err := db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle())); err != nil {
		return err
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + tableName).Scan(&count); err != nil {
		return err
	}
	if count != expectedRows {
		return fmt.Errorf("unexpected row count for %s: expected %d, got %d", tableName, expectedRows, count)
	}

	if preview > 0 {
		query := fmt.Sprintf("SELECT id, name, age, country FROM %s ORDER BY id LIMIT %d", tableName, preview)
		rows, err := db.Query(query)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var id int64
			var name string
			var age int64
			var country string
			if err := rows.Scan(&id, &name, &age, &country); err != nil {
				return err
			}
			fmt.Printf("  row id=%d name=%q age=%d country=%s\n", id, name, age, country)
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}

	return nil
}
