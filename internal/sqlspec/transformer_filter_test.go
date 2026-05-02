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

func TestDeriveFilter_ValidEqFilter_BuildsWhere(t *testing.T) {
	t.Parallel()

	base := TableSpec{
		Name: "people",
		Columns: []ColumnSpec{
			{Name: "id", SQLType: "integer"},
			{Name: "name", SQLType: "varchar"},
		},
	}

	resolved, stmt, err := DeriveFilter(base, "people_filtered", FilterRow{
		Column:    "name",
		Operation: FilterOperationEq,
		Value:     "John",
	})

	require.NoError(t, err)
	require.NotNil(t, stmt)
	assert.Equal(t, "people_filtered", resolved.Table.Name)
	assert.Len(t, resolved.Table.Columns, 2)

	query, ok := stmt.Query.(*ast.SelectStatement)
	require.True(t, ok)

	where, ok := query.Where.(*ast.BinaryExpression)
	require.True(t, ok)

	left, ok := where.Left.(*ast.Identifier)
	require.True(t, ok)
	assert.Equal(t, "name", left.Name)
	assert.Equal(t, "=", where.Operator)

	right, ok := where.Right.(*ast.LiteralValue)
	require.True(t, ok)
	assert.Equal(t, "John", right.Value)
	assert.Equal(t, "STRING", right.Type)
}

func TestDeriveFilter_BaseNameRequired(t *testing.T) {
	t.Parallel()

	_, _, err := DeriveFilter(TableSpec{}, "people_filtered", FilterRow{
		Column:    "name",
		Operation: FilterOperationEq,
		Value:     "John",
	})
	assert.Error(t, err)
}

func TestDeriveFilter_DerivedTableNameRequired(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "name", SQLType: "varchar"}}}
	_, _, err := DeriveFilter(base, "", FilterRow{
		Column:    "name",
		Operation: FilterOperationEq,
		Value:     "John",
	})
	assert.Error(t, err)
}

func TestDeriveFilter_ColumnMustExist(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}
	_, _, err := DeriveFilter(base, "people_filtered", FilterRow{
		Column:    "name",
		Operation: FilterOperationEq,
		Value:     "John",
	})
	assert.Error(t, err)
}

func TestDeriveFilter_UnsupportedOperation(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "name", SQLType: "varchar"}}}
	_, _, err := DeriveFilter(base, "people_filtered", FilterRow{
		Column:    "name",
		Operation: FilterOperation("neq"),
		Value:     "John",
	})
	assert.Error(t, err)
}

func TestDeriveFilter_DuckDBBackfillFiltersRows(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	base := TableSpec{
		Name: "people",
		Columns: []ColumnSpec{
			{Name: "id", SQLType: "integer"},
			{Name: "name", SQLType: "varchar"},
		},
	}

	createBase, err := BuildCreateTable(base)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createBase, ast.CompactStyle()))
	require.NoError(t, err)

	insertBase, err := BuildInsert(InsertSpec{
		Table:   "people",
		Columns: []string{"id", "name"},
		Rows: []map[string]any{
			{"id": 1, "name": "John"},
			{"id": 2, "name": "Doe"},
			{"id": 3, "name": "John"},
		},
	})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insertBase, ast.CompactStyle()))
	require.NoError(t, err)

	resolved, backfill, err := DeriveFilter(base, "people_filtered", FilterRow{
		Column:    "name",
		Operation: FilterOperationEq,
		Value:     "John",
	})
	require.NoError(t, err)

	createDerived, err := BuildCreateTable(resolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createDerived, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
	require.NoError(t, err)

	rows, err := db.Query("SELECT id, name FROM people_filtered ORDER BY id")
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
	assert.Equal(t, []int{1, 3}, ids)
	assert.Equal(t, []string{"John", "John"}, names)
}
