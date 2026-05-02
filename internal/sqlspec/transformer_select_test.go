package sqlspec

import (
	"database/sql"
	"testing"

	"github.com/ajitpratap0/GoSQLX/pkg/formatter"
	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeriveSelect_ValidSelection_BuildsResolvedAndInsert(t *testing.T) {
	t.Parallel()

	base := TableSpec{
		Name: "people",
		Columns: []ColumnSpec{
			{Name: "id", SQLType: "integer"},
			{Name: "name", SQLType: "varchar"},
			{Name: "age", SQLType: "integer"},
		},
	}

	resolved, stmt, err := DeriveSelect(base, "people_selected", []string{"id", "name"})
	require.NoError(t, err)
	require.NotNil(t, stmt)

	assert.Equal(t, "people_selected", resolved.Table.Name)
	require.Len(t, resolved.Table.Columns, 2)
	assert.Equal(t, "id", resolved.Table.Columns[0].Name)
	assert.Equal(t, "name", resolved.Table.Columns[1].Name)

	assert.Equal(t, "people_selected", stmt.TableName)

	query, ok := stmt.Query.(*ast.SelectStatement)
	require.True(t, ok)
	require.Len(t, query.From, 1)
	assert.Equal(t, "people", query.From[0].Name)
	require.Len(t, query.Columns, 2)

	col0, ok := query.Columns[0].(*ast.Identifier)
	require.True(t, ok)
	assert.Equal(t, "id", col0.Name)

	col1, ok := query.Columns[1].(*ast.Identifier)
	require.True(t, ok)
	assert.Equal(t, "name", col1.Name)
}

func TestDeriveSelect_BaseNameRequired(t *testing.T) {
	t.Parallel()

	_, _, err := DeriveSelect(TableSpec{}, "people_selected", []string{"id"})
	assert.Error(t, err)
}

func TestDeriveSelect_DerivedTableNameRequired(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}
	_, _, err := DeriveSelect(base, "", []string{"id"})
	assert.Error(t, err)
}

func TestDeriveSelect_ColumnsRequired(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}
	_, _, err := DeriveSelect(base, "people_selected", nil)
	assert.Error(t, err)
}

func TestDeriveSelect_InvalidColumn_ReturnsError(t *testing.T) {
	t.Parallel()

	base := TableSpec{
		Name: "people",
		Columns: []ColumnSpec{
			{Name: "id", SQLType: "integer"},
			{Name: "name", SQLType: "varchar"},
		},
	}

	_, _, err := DeriveSelect(base, "people_selected", []string{"id", "unknown"})
	assert.Error(t, err)
}

func TestDeriveSelect_CaseInsensitiveColumnMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		baseColumn     string
		selectedColumn string
	}{
		{name: "uppercase base lowercase select", baseColumn: "Name", selectedColumn: "name"},
		{name: "id and Id", baseColumn: "Id", selectedColumn: "id"},
		{name: "id and ID", baseColumn: "id", selectedColumn: "ID"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: tc.baseColumn, SQLType: "varchar"}}}
			resolved, _, err := DeriveSelect(base, "people_selected", []string{tc.selectedColumn})
			require.NoError(t, err)
			require.Len(t, resolved.Table.Columns, 1)
			assert.Equal(t, tc.baseColumn, resolved.Table.Columns[0].Name)
		})
	}
}

func TestDeriveSelect_Regression_ValidSingleColumnDoesNotFail(t *testing.T) {
	t.Parallel()

	base := TableSpec{
		Name: "people",
		Columns: []ColumnSpec{
			{Name: "id", SQLType: "integer"},
			{Name: "name", SQLType: "varchar"},
		},
	}

	resolved, stmt, err := DeriveSelect(base, "people_selected", []string{"id"})
	require.NoError(t, err)
	require.NotNil(t, stmt)
	require.Len(t, resolved.Table.Columns, 1)
	assert.Equal(t, "id", resolved.Table.Columns[0].Name)
}

func TestDeriveSelect_DuckDBBackfillProjectsColumns(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	base := TableSpec{
		Name: "people",
		Columns: []ColumnSpec{
			{Name: "id", SQLType: "integer"},
			{Name: "name", SQLType: "varchar"},
			{Name: "age", SQLType: "integer"},
		},
	}

	createBase, err := BuildCreateTable(base)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createBase, ast.CompactStyle()))
	require.NoError(t, err)

	insertBase, err := BuildInsert(InsertSpec{
		Table:   "people",
		Columns: []string{"id", "name", "age"},
		Rows: []map[string]any{
			{"id": 1, "name": "John", "age": 30},
			{"id": 2, "name": "Doe", "age": 25},
		},
	})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insertBase, ast.CompactStyle()))
	require.NoError(t, err)

	resolved, backfill, err := DeriveSelect(base, "people_selected", []string{"id", "name"})
	require.NoError(t, err)

	createDerived, err := BuildCreateTable(resolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createDerived, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
	require.NoError(t, err)

	rows, err := db.Query("SELECT id, name FROM people_selected ORDER BY id")
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

func TestDeriveSelect_DuckDBBackfill_CaseInsensitiveSelectedColumn(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	base := TableSpec{
		Name: "people",
		Columns: []ColumnSpec{
			{Name: "Id", SQLType: "integer"},
			{Name: "Name", SQLType: "varchar"},
		},
	}

	createBase, err := BuildCreateTable(base)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createBase, ast.CompactStyle()))
	require.NoError(t, err)

	insertBase, err := BuildInsert(InsertSpec{
		Table:   "people",
		Columns: []string{"Id", "Name"},
		Rows: []map[string]any{
			{"Id": 1, "Name": "John"},
			{"Id": 2, "Name": "Doe"},
		},
	})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insertBase, ast.CompactStyle()))
	require.NoError(t, err)

	resolved, backfill, err := DeriveSelect(base, "people_selected", []string{"id"})
	require.NoError(t, err)
	require.Len(t, resolved.Table.Columns, 1)
	assert.Equal(t, "Id", resolved.Table.Columns[0].Name)

	createDerived, err := BuildCreateTable(resolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createDerived, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
	require.NoError(t, err)

	rows, err := db.Query("SELECT id FROM people_selected ORDER BY id")
	require.NoError(t, err)
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		err := rows.Scan(&id)
		require.NoError(t, err)
		ids = append(ids, id)
	}

	require.NoError(t, rows.Err())
	assert.Equal(t, []int{1, 2}, ids)
}
