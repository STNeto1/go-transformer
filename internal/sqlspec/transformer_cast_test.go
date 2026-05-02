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

func TestDeriveCastColumns_ValidSingleCast(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "varchar"}, {Name: "name", SQLType: "varchar"}}}
	resolved, stmt, err := DeriveCastColumns(base, "people_casted", []CastColumn{{Column: "id", SQLType: "integer"}})
	require.NoError(t, err)
	require.NotNil(t, stmt)

	assert.Equal(t, "people_casted", resolved.Table.Name)
	require.Len(t, resolved.Table.Columns, 2)
	assert.Equal(t, "integer", resolved.Table.Columns[0].SQLType)
	assert.Equal(t, "varchar", resolved.Table.Columns[1].SQLType)

	query := stmt.Query.(*ast.SelectStatement)
	require.Len(t, query.Columns, 2)

	castExpr, ok := query.Columns[0].(*ast.CastExpression)
	require.True(t, ok)
	assert.Equal(t, "integer", castExpr.Type)
	assert.False(t, castExpr.Try)
	inner, ok := castExpr.Expr.(*ast.Identifier)
	require.True(t, ok)
	assert.Equal(t, "id", inner.Name)

	col, ok := query.Columns[1].(*ast.Identifier)
	require.True(t, ok)
	assert.Equal(t, "name", col.Name)
}

func TestDeriveCastColumns_ValidMultipleCasts(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "varchar"}, {Name: "age", SQLType: "varchar"}}}
	resolved, _, err := DeriveCastColumns(base, "people_casted", []CastColumn{{Column: "id", SQLType: "integer"}, {Column: "age", SQLType: "integer"}})
	require.NoError(t, err)
	assert.Equal(t, "integer", resolved.Table.Columns[0].SQLType)
	assert.Equal(t, "integer", resolved.Table.Columns[1].SQLType)
}

func TestDeriveCastColumns_CaseInsensitiveColumnMatch(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "ID", SQLType: "varchar"}}}
	_, stmt, err := DeriveCastColumns(base, "people_casted", []CastColumn{{Column: "id", SQLType: "integer"}})
	require.NoError(t, err)

	query := stmt.Query.(*ast.SelectStatement)
	castExpr := query.Columns[0].(*ast.CastExpression)
	inner := castExpr.Expr.(*ast.Identifier)
	assert.Equal(t, "ID", inner.Name)
}

func TestDeriveCastColumns_InvalidInputs(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "varchar"}, {Name: "name", SQLType: "varchar"}}}
	tests := []struct {
		name  string
		base  TableSpec
		table string
		casts []CastColumn
	}{
		{name: "base required", base: TableSpec{}, table: "x", casts: []CastColumn{{Column: "id", SQLType: "integer"}}},
		{name: "table required", base: base, table: "", casts: []CastColumn{{Column: "id", SQLType: "integer"}}},
		{name: "casts required", base: base, table: "x", casts: nil},
		{name: "column required", base: base, table: "x", casts: []CastColumn{{Column: "", SQLType: "integer"}}},
		{name: "type required", base: base, table: "x", casts: []CastColumn{{Column: "id", SQLType: ""}}},
		{name: "unknown column", base: base, table: "x", casts: []CastColumn{{Column: "foo", SQLType: "integer"}}},
		{name: "duplicate column", base: base, table: "x", casts: []CastColumn{{Column: "id", SQLType: "integer"}, {Column: "ID", SQLType: "bigint"}}},
		{name: "no-op cast", base: base, table: "x", casts: []CastColumn{{Column: "id", SQLType: "VARCHAR"}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := DeriveCastColumns(tc.base, tc.table, tc.casts)
			assert.Error(t, err)
		})
	}
}

func TestDeriveCastColumns_DuckDBBackfillCastsValues(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "varchar"}, {Name: "name", SQLType: "varchar"}}}

	createBase, err := BuildCreateTable(base)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createBase, ast.CompactStyle()))
	require.NoError(t, err)

	insertBase, err := BuildInsert(InsertSpec{Table: "people", Columns: []string{"id", "name"}, Rows: []map[string]any{{"id": "1", "name": "John"}, {"id": "2", "name": "Doe"}}})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insertBase, ast.CompactStyle()))
	require.NoError(t, err)

	resolved, backfill, err := DeriveCastColumns(base, "people_casted", []CastColumn{{Column: "id", SQLType: "integer"}})
	require.NoError(t, err)

	createDerived, err := BuildCreateTable(resolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createDerived, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
	require.NoError(t, err)

	rows, err := db.Query("SELECT id, name FROM people_casted ORDER BY id")
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

func TestDeriveCastColumns_DuckDBBackfill_InvalidCastFails(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "varchar"}}}

	createBase, err := BuildCreateTable(base)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createBase, ast.CompactStyle()))
	require.NoError(t, err)

	insertBase, err := BuildInsert(InsertSpec{Table: "people", Columns: []string{"id"}, Rows: []map[string]any{{"id": "abc"}}})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insertBase, ast.CompactStyle()))
	require.NoError(t, err)

	resolved, backfill, err := DeriveCastColumns(base, "people_casted_fail", []CastColumn{{Column: "id", SQLType: "integer"}})
	require.NoError(t, err)

	createDerived, err := BuildCreateTable(resolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createDerived, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
	assert.Error(t, err)
}
