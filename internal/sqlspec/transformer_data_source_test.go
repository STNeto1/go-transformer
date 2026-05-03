package sqlspec

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/ajitpratap0/GoSQLX/pkg/formatter"
	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeriveDataSource_InvalidInputs(t *testing.T) {
	t.Parallel()

	trueVal := true
	declared := []ColumnSpec{{Name: "id", SQLType: "INTEGER"}}

	tests := []struct {
		name  string
		table string
		spec  DataSourceSpec
		db    *sql.DB
	}{
		{name: "table required", table: "", spec: DataSourceSpec{Format: DataSourceFormatCSV, Path: "x.csv", Mode: DataSourceModeInfer}},
		{name: "path required", table: "x", spec: DataSourceSpec{Format: DataSourceFormatCSV, Path: "", Mode: DataSourceModeInfer}},
		{name: "format required", table: "x", spec: DataSourceSpec{Format: "", Path: "x.csv", Mode: DataSourceModeInfer}},
		{name: "invalid mode", table: "x", spec: DataSourceSpec{Format: DataSourceFormatCSV, Path: "x.csv", Mode: "bad"}},
		{name: "declared needs columns", table: "x", spec: DataSourceSpec{Format: DataSourceFormatCSV, Path: "x.csv", Mode: DataSourceModeDeclared}},
		{name: "check needs columns", table: "x", spec: DataSourceSpec{Format: DataSourceFormatCSV, Path: "x.csv", Mode: DataSourceModeDeclaredWithCheck}},
		{name: "parquet csv option disallowed", table: "x", spec: DataSourceSpec{Format: DataSourceFormatParquet, Path: "x.parquet", Mode: DataSourceModeDeclared, Columns: declared, CSVHeader: &trueVal}},
		{name: "infer mode requires db", table: "x", spec: DataSourceSpec{Format: DataSourceFormatCSV, Path: "x.csv", Mode: DataSourceModeInfer}, db: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := DeriveDataSource(tc.table, tc.spec, tc.db)
			assert.Error(t, err)
		})
	}
}

func TestDeriveDataSource_DeclaredModeBuildsInsertWithCasts(t *testing.T) {
	t.Parallel()

	spec := DataSourceSpec{
		Format: DataSourceFormatCSV,
		Path:   "/tmp/people.csv",
		Mode:   DataSourceModeDeclared,
		Columns: []ColumnSpec{
			{Name: "id", SQLType: "INTEGER"},
			{Name: "name", SQLType: "VARCHAR"},
		},
	}

	resolved, stmt, err := DeriveDataSource("people_src", spec, nil)
	require.NoError(t, err)
	require.NotNil(t, stmt)
	assert.Equal(t, "people_src", resolved.Table.Name)
	require.Len(t, resolved.Table.Columns, 2)

	query, ok := stmt.Query.(*ast.SelectStatement)
	require.True(t, ok)
	require.Len(t, query.From, 1)
	assert.Contains(t, query.From[0].Name, "read_csv_auto")
	require.Len(t, query.Columns, 2)
	_, ok = query.Columns[0].(*ast.CastExpression)
	assert.True(t, ok)
}

func TestDeriveDataSource_DuckDBInferCSV(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	dir := t.TempDir()
	csvPath := filepath.Join(dir, "people.csv")
	err = os.WriteFile(csvPath, []byte("id,name\n1,John\n2,Doe\n"), 0o644)
	require.NoError(t, err)

	spec := DataSourceSpec{Format: DataSourceFormatCSV, Path: csvPath, Mode: DataSourceModeInfer}
	resolved, backfill, err := DeriveDataSource("people_src", spec, db)
	require.NoError(t, err)

	createDerived, err := BuildCreateTable(resolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createDerived, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
	require.NoError(t, err)

	rows, err := db.Query("SELECT id, name FROM people_src ORDER BY id")
	require.NoError(t, err)
	defer rows.Close()

	var ids []int
	var names []string
	for rows.Next() {
		var id int
		var name string
		err := rows.Scan(&id, &name)
		require.NoError(t, err)
		ids = append(ids, id)
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []int{1, 2}, ids)
	assert.Equal(t, []string{"John", "Doe"}, names)
}

func TestDeriveDataSource_DuckDBInferParquet(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	dir := t.TempDir()
	parquetPath := filepath.Join(dir, "people.parquet")

	_, err = db.Exec("CREATE TABLE people_stage (id INTEGER, name VARCHAR)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO people_stage VALUES (1, 'John'), (2, 'Doe')")
	require.NoError(t, err)
	_, err = db.Exec("COPY people_stage TO " + sqlStringLiteral(parquetPath) + " (FORMAT PARQUET)")
	require.NoError(t, err)

	spec := DataSourceSpec{Format: DataSourceFormatParquet, Path: parquetPath, Mode: DataSourceModeInfer}
	resolved, backfill, err := DeriveDataSource("people_src", spec, db)
	require.NoError(t, err)

	createDerived, err := BuildCreateTable(resolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createDerived, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
	require.NoError(t, err)

	rows, err := db.Query("SELECT id, name FROM people_src ORDER BY id")
	require.NoError(t, err)
	defer rows.Close()

	var count int
	for rows.Next() {
		count++
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, 2, count)
}

func TestDeriveDataSource_DuckDBDeclaredWithStrictCheckMismatch(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	dir := t.TempDir()
	csvPath := filepath.Join(dir, "people.csv")
	err = os.WriteFile(csvPath, []byte("id,name\n1,John\n"), 0o644)
	require.NoError(t, err)

	_, _, err = DeriveDataSource("people_src", DataSourceSpec{
		Format: DataSourceFormatCSV,
		Path:   csvPath,
		Mode:   DataSourceModeDeclaredWithCheck,
		Columns: []ColumnSpec{
			{Name: "name", SQLType: "VARCHAR"},
			{Name: "id", SQLType: "INTEGER"},
		},
	}, db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name mismatch")
}

func TestDeriveDataSource_DuckDBDeclaredWithStrictCheckSuccess(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	dir := t.TempDir()
	csvPath := filepath.Join(dir, "people.csv")
	err = os.WriteFile(csvPath, []byte("id,name\n1,John\n2,Doe\n"), 0o644)
	require.NoError(t, err)

	resolved, backfill, err := DeriveDataSource("people_src", DataSourceSpec{
		Format: DataSourceFormatCSV,
		Path:   csvPath,
		Mode:   DataSourceModeDeclaredWithCheck,
		Columns: []ColumnSpec{
			{Name: "id", SQLType: "BIGINT"},
			{Name: "name", SQLType: "VARCHAR"},
		},
	}, db)
	require.NoError(t, err)

	createDerived, err := BuildCreateTable(resolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createDerived, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
	require.NoError(t, err)

	row := db.QueryRow("SELECT COUNT(*) FROM people_src")
	var count int
	err = row.Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}
