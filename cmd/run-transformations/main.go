package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ajitpratap0/GoSQLX/pkg/formatter"
	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
	_ "github.com/duckdb/duckdb-go/v2"

	"go-transformer/internal/sqlspec"
)

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
	payload := map[string]any{"ts": time.Now().UTC().Format(time.RFC3339Nano), "event": event}
	maps.Copy(payload, fields)

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
	if t.level == "debug" {
		t.emit(event, fields)
	}
}

type runnerCtx struct {
	db        *sql.DB
	base      sqlspec.TableSpec
	preview   int
	telemetry *telemetryLogger
}

type scenario struct {
	name            string
	expectedFailure bool
	run             func(*runnerCtx) error
}

func main() {
	preview := flag.Int("preview", 5, "rows to preview per successful scenario")
	dataDir := flag.String("data-dir", "data", "directory containing generated data files")
	telemetryFormat := flag.String("telemetry-format", "text", "telemetry output format: text|json")
	telemetryLevel := flag.String("telemetry-level", "basic", "telemetry verbosity: basic|debug")
	flag.Parse()

	telemetry := newTelemetryLogger(*telemetryFormat, *telemetryLevel)
	runStartedAt := time.Now()

	csvPath := filepath.Join(*dataDir, "people.csv")
	parquetPath := filepath.Join(*dataDir, "people.parquet")
	telemetry.emit("run_start", map[string]any{"preview": *preview, "data_dir": *dataDir, "csv_path": csvPath, "parquet_path": parquetPath})

	if _, err := os.Stat(csvPath); err != nil {
		log.Fatalf("required input file missing: %s (run `make gen-data` first)", csvPath)
	}
	if _, err := os.Stat(parquetPath); err != nil {
		log.Fatalf("required input file missing: %s (run `make gen-data` first)", parquetPath)
	}

	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	base, err := loadBaseTable(db, csvPath)
	if err != nil {
		log.Fatal(err)
	}

	ctx := &runnerCtx{db: db, base: base, preview: *preview, telemetry: telemetry}

	scenarios := []scenario{
		{name: "select_columns", run: runSelectColumns},
		{name: "filter_rows", run: runFilterRows},
		{name: "sort_then_limit", run: runSortThenLimit},
		{name: "constant_column", run: runConstantColumn},
		{name: "compute_column", run: runComputeColumn},
		{name: "rename_columns", run: runRenameColumns},
		{name: "cast_columns", run: runCastColumns},
		{name: "aggregate_country", run: runAggregateCountry},
	}

	passed := 0
	failed := 0
	for _, sc := range scenarios {
		fmt.Printf("\n")

		started := time.Now()
		telemetry.emit("scenario_start", map[string]any{"scenario": sc.name})
		err := sc.run(ctx)
		durationMS := time.Since(started).Milliseconds()

		switch {
		case err == nil && !sc.expectedFailure:
			passed++
			fmt.Printf("PASS: %s\n", sc.name)
			telemetry.emit("scenario_end", map[string]any{"scenario": sc.name, "status": "pass", "duration_ms": durationMS})
		case err != nil && sc.expectedFailure:
			passed++
			fmt.Printf("PASS (expected failure): %s -> %v\n", sc.name, err)
			telemetry.emit("scenario_end", map[string]any{"scenario": sc.name, "status": "expected_fail", "duration_ms": durationMS, "error": err.Error()})
		default:
			failed++
			fmt.Printf("FAIL: %s -> %v\n", sc.name, err)
			telemetry.emit("scenario_end", map[string]any{"scenario": sc.name, "status": "fail", "duration_ms": durationMS, "error": err.Error()})
		}

		fmt.Printf("\n")
	}

	fmt.Printf("\nSummary: passed=%d failed=%d\n", passed, failed)
	telemetry.emit("run_end", map[string]any{"passed": passed, "failed": failed, "duration_ms": time.Since(runStartedAt).Milliseconds()})
	if failed > 0 {
		os.Exit(1)
	}
}

func loadBaseTable(db *sql.DB, csvPath string) (sqlspec.TableSpec, error) {
	resolved, backfill, err := sqlspec.DeriveDataSource("people_base", sqlspec.DataSourceSpec{
		Format: sqlspec.DataSourceFormatCSV,
		Path:   csvPath,
		Mode:   sqlspec.DataSourceModeInfer,
	}, db)
	if err != nil {
		return sqlspec.TableSpec{}, err
	}

	if _, err := db.Exec("DROP TABLE IF EXISTS people_base"); err != nil {
		return sqlspec.TableSpec{}, err
	}
	createStmt, err := sqlspec.BuildCreateTable(resolved.Table)
	if err != nil {
		return sqlspec.TableSpec{}, err
	}
	if _, err := db.Exec(formatter.FormatStatement(createStmt, ast.CompactStyle())); err != nil {
		return sqlspec.TableSpec{}, err
	}
	if _, err := db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle())); err != nil {
		return sqlspec.TableSpec{}, err
	}

	return resolved.Table, nil
}

func runSelectColumns(ctx *runnerCtx) error {
	resolved, stmt, err := sqlspec.DeriveSelect(ctx.base, "tr_select", []string{"id", "name", "country"})
	if err != nil {
		return err
	}
	return materializeAndPreview(ctx, resolved.Table, stmt)
}

func runFilterRows(ctx *runnerCtx) error {
	resolved, stmt, err := sqlspec.DeriveFilter(ctx.base, "tr_filter", sqlspec.FilterRow{Column: "age", Operation: sqlspec.FilterOperationGt, Value: 40})
	if err != nil {
		return err
	}
	return materializeAndPreview(ctx, resolved.Table, stmt)
}

func runSortThenLimit(ctx *runnerCtx) error {
	sorted, sortStmt, err := sqlspec.DeriveSort(ctx.base, "tr_sort", []sqlspec.SortKey{{Column: "age", Direction: sqlspec.SortDirectionDesc}})
	if err != nil {
		return err
	}
	if err := materializeAndPreview(ctx, sorted.Table, sortStmt); err != nil {
		return err
	}

	limited, limitStmt, err := sqlspec.DeriveLimit(sorted.Table, "tr_sort_limit", sqlspec.LimitSpec{Count: 5})
	if err != nil {
		return err
	}
	return materializeAndPreview(ctx, limited.Table, limitStmt)
}

func runConstantColumn(ctx *runnerCtx) error {
	resolved, stmt, err := sqlspec.DeriveConstantColumn(ctx.base, "tr_constant", sqlspec.ColumnSpec{Name: "source", SQLType: "VARCHAR"}, "demo")
	if err != nil {
		return err
	}
	return materializeAndPreview(ctx, resolved.Table, stmt)
}

func runComputeColumn(ctx *runnerCtx) error {
	resolved, stmt, err := sqlspec.DeriveComputeColumns(ctx.base, "tr_compute", []sqlspec.ComputeColumn{{Name: "label", SQLType: "VARCHAR", Expr: "{{name}} from {{country}}"}})
	if err != nil {
		return err
	}
	return materializeAndPreview(ctx, resolved.Table, stmt)
}

func runRenameColumns(ctx *runnerCtx) error {
	resolved, stmt, err := sqlspec.DeriveRenameColumns(ctx.base, "tr_rename", []sqlspec.RenameColumn{{From: "name", To: "full_name"}})
	if err != nil {
		return err
	}
	return materializeAndPreview(ctx, resolved.Table, stmt)
}

func runCastColumns(ctx *runnerCtx) error {
	resolved, stmt, err := sqlspec.DeriveCastColumns(ctx.base, "tr_cast", []sqlspec.CastColumn{{Column: "age", SQLType: "VARCHAR"}})
	if err != nil {
		return err
	}
	return materializeAndPreview(ctx, resolved.Table, stmt)
}

func runAggregateCountry(ctx *runnerCtx) error {
	resolved, stmt, err := sqlspec.DeriveAggregate(ctx.base, "tr_agg", sqlspec.AggregateSpec{
		GroupBy: []string{"country"},
		Metrics: []sqlspec.AggregateMetric{{Function: sqlspec.AggregateFunctionCount, Column: "*", As: "total"}},
	})
	if err != nil {
		return err
	}
	return materializeAndPreview(ctx, resolved.Table, stmt)
}

func materializeAndPreview(ctx *runnerCtx, table sqlspec.TableSpec, stmt *ast.InsertStatement) error {
	if _, err := ctx.db.Exec("DROP TABLE IF EXISTS " + table.Name); err != nil {
		return err
	}
	createStmt, err := sqlspec.BuildCreateTable(table)
	if err != nil {
		return err
	}
	if _, err := ctx.db.Exec(formatter.FormatStatement(createStmt, ast.CompactStyle())); err != nil {
		return err
	}
	if _, err := ctx.db.Exec(formatter.FormatStatement(stmt, ast.CompactStyle())); err != nil {
		return err
	}

	var count int
	if err := ctx.db.QueryRow("SELECT COUNT(*) FROM " + table.Name).Scan(&count); err != nil {
		return err
	}
	ctx.telemetry.debug("scenario_table", map[string]any{"table": table.Name, "rows": count})

	if ctx.preview <= 0 {
		return nil
	}

	rows, err := ctx.db.Query(fmt.Sprintf("SELECT * FROM %s LIMIT %d", table.Name, ctx.preview))
	if err != nil {
		return err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		line := make([]string, 0, len(cols))
		for i, c := range cols {
			line = append(line, fmt.Sprintf("%s=%v", c, values[i]))
		}
		fmt.Printf("  row %s\n", strings.Join(line, " "))
	}

	return rows.Err()
}
