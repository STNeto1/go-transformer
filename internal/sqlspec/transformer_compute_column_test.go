package sqlspec

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/ajitpratap0/GoSQLX/pkg/formatter"
	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeriveComputeColumns_TemplateAndRawExpression(t *testing.T) {
	t.Parallel()

	base := TableSpec{
		Name: "people",
		Columns: []ColumnSpec{
			{Name: "first_name", SQLType: "varchar"},
			{Name: "last_name", SQLType: "varchar"},
			{Name: "col", SQLType: "integer"},
		},
	}

	resolved, stmt, err := DeriveComputeColumns(base, "people_computed", []ComputeColumn{
		{Name: "fullname", SQLType: "varchar", Expr: "{{first_name}} {{last_name}}"},
		{Name: "col_x2", SQLType: "integer", Expr: "col * 2"},
	})
	require.NoError(t, err)
	require.NotNil(t, stmt)

	assert.Equal(t, "people_computed", resolved.Table.Name)
	require.Len(t, resolved.Table.Columns, 5)
	assert.Equal(t, "fullname", resolved.Table.Columns[3].Name)
	assert.Equal(t, "col_x2", resolved.Table.Columns[4].Name)

	query, ok := stmt.Query.(*ast.SelectStatement)
	require.True(t, ok)
	require.Len(t, query.Columns, 5)

	concatExpr, ok := query.Columns[3].(*ast.FunctionCall)
	require.True(t, ok)
	assert.Equal(t, "CONCAT", concatExpr.Name)
	require.Len(t, concatExpr.Arguments, 3)

	id0, ok := concatExpr.Arguments[0].(*ast.Identifier)
	require.True(t, ok)
	assert.Equal(t, "first_name", id0.Name)

	lit1, ok := concatExpr.Arguments[1].(*ast.LiteralValue)
	require.True(t, ok)
	assert.Equal(t, " ", lit1.Value)

	id2, ok := concatExpr.Arguments[2].(*ast.Identifier)
	require.True(t, ok)
	assert.Equal(t, "last_name", id2.Name)

	multiplyExpr, ok := query.Columns[4].(*ast.BinaryExpression)
	require.True(t, ok)
	assert.Equal(t, "*", multiplyExpr.Operator)
}

func TestDeriveComputeColumns_CaseInsensitiveTemplateColumnMatch(t *testing.T) {
	t.Parallel()

	base := TableSpec{
		Name:    "people",
		Columns: []ColumnSpec{{Name: "First_Name", SQLType: "varchar"}, {Name: "Last_Name", SQLType: "varchar"}},
	}

	_, stmt, err := DeriveComputeColumns(base, "people_computed", []ComputeColumn{
		{Name: "fullname", SQLType: "varchar", Expr: "{{first_name}} {{last_name}}"},
	})
	require.NoError(t, err)

	query := stmt.Query.(*ast.SelectStatement)
	concatExpr := query.Columns[2].(*ast.FunctionCall)
	id0 := concatExpr.Arguments[0].(*ast.Identifier)
	id2 := concatExpr.Arguments[2].(*ast.Identifier)
	assert.Equal(t, "First_Name", id0.Name)
	assert.Equal(t, "Last_Name", id2.Name)
}

func TestDeriveComputeColumns_InvalidInputs(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "col", SQLType: "integer"}}}

	tests := []struct {
		name     string
		base     TableSpec
		table    string
		computes []ComputeColumn
	}{
		{name: "base required", base: TableSpec{}, table: "x", computes: []ComputeColumn{{Name: "c", SQLType: "integer", Expr: "col * 2"}}},
		{name: "table required", base: base, table: "", computes: []ComputeColumn{{Name: "c", SQLType: "integer", Expr: "col * 2"}}},
		{name: "computes required", base: base, table: "x", computes: nil},
		{name: "name required", base: base, table: "x", computes: []ComputeColumn{{Name: "", SQLType: "integer", Expr: "col * 2"}}},
		{name: "sql type required", base: base, table: "x", computes: []ComputeColumn{{Name: "c", SQLType: "", Expr: "col * 2"}}},
		{name: "expr required", base: base, table: "x", computes: []ComputeColumn{{Name: "c", SQLType: "integer", Expr: "   "}}},
		{name: "target collides", base: base, table: "x", computes: []ComputeColumn{{Name: "Col", SQLType: "integer", Expr: "col * 2"}}},
		{name: "duplicate target", base: base, table: "x", computes: []ComputeColumn{{Name: "c", SQLType: "integer", Expr: "col * 2"}, {Name: "C", SQLType: "integer", Expr: "col * 3"}}},
		{name: "template unknown ref", base: base, table: "x", computes: []ComputeColumn{{Name: "c", SQLType: "varchar", Expr: "{{missing}}"}}},
		{name: "raw unknown ref", base: base, table: "x", computes: []ComputeColumn{{Name: "c", SQLType: "integer", Expr: "missing * 2"}}},
		{name: "raw invalid syntax", base: base, table: "x", computes: []ComputeColumn{{Name: "c", SQLType: "integer", Expr: "col *"}}},
		{name: "same-node computed ref", base: base, table: "x", computes: []ComputeColumn{{Name: "col_x2", SQLType: "integer", Expr: "col * 2"}, {Name: "col_x4", SQLType: "integer", Expr: "col_x2 * 2"}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := DeriveComputeColumns(tc.base, tc.table, tc.computes)
			assert.Error(t, err)
		})
	}
}

func TestDeriveComputeColumns_DuckDBBackfillComputesValues(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	base := TableSpec{
		Name: "people",
		Columns: []ColumnSpec{
			{Name: "id", SQLType: "integer"},
			{Name: "first_name", SQLType: "varchar"},
			{Name: "last_name", SQLType: "varchar"},
			{Name: "col", SQLType: "integer"},
		},
	}

	createBase, err := BuildCreateTable(base)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createBase, ast.CompactStyle()))
	require.NoError(t, err)

	insertBase, err := BuildInsert(InsertSpec{
		Table:   "people",
		Columns: []string{"id", "first_name", "last_name", "col"},
		Rows: []map[string]any{
			{"id": 1, "first_name": "John", "last_name": "Doe", "col": 3},
			{"id": 2, "first_name": "Mary", "last_name": "Jane", "col": 5},
		},
	})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insertBase, ast.CompactStyle()))
	require.NoError(t, err)

	resolved, backfill, err := DeriveComputeColumns(base, "people_computed", []ComputeColumn{
		{Name: "fullname", SQLType: "varchar", Expr: "{{first_name}} {{last_name}}"},
		{Name: "col_x2", SQLType: "integer", Expr: "col * 2"},
	})
	require.NoError(t, err)

	createDerived, err := BuildCreateTable(resolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createDerived, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
	require.NoError(t, err)

	rows, err := db.Query("SELECT id, fullname, col_x2 FROM people_computed ORDER BY id")
	require.NoError(t, err)
	defer rows.Close()

	var got []string
	for rows.Next() {
		var id int
		var fullname string
		var colX2 int
		err := rows.Scan(&id, &fullname, &colX2)
		require.NoError(t, err)
		got = append(got, fmt.Sprintf("%s:%d", fullname, colX2))
	}

	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"John Doe:6", "Mary Jane:10"}, got)
}
