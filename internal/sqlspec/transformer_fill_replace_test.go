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

func TestDeriveFillReplace_FillOnly_BuildsCoalesce(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}
	_, stmt, err := DeriveFillReplace(base, "people_filled", []FillReplaceRule{{Column: "name", FillNullWith: "unknown"}})
	require.NoError(t, err)

	query := stmt.Query.(*ast.SelectStatement)
	require.Len(t, query.Columns, 2)

	_, ok := query.Columns[0].(*ast.Identifier)
	require.True(t, ok)

	fn, ok := query.Columns[1].(*ast.FunctionCall)
	require.True(t, ok)
	assert.Equal(t, "COALESCE", fn.Name)
	require.Len(t, fn.Arguments, 2)
}

func TestDeriveFillReplace_ReplaceOnly_BuildsCase(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "status", SQLType: "varchar"}}}
	_, stmt, err := DeriveFillReplace(base, "people_replaced", []FillReplaceRule{{Column: "status", ReplaceFrom: "N/A", ReplaceTo: "unknown"}})
	require.NoError(t, err)

	query := stmt.Query.(*ast.SelectStatement)
	require.Len(t, query.Columns, 1)

	ce, ok := query.Columns[0].(*ast.CaseExpression)
	require.True(t, ok)
	require.Len(t, ce.WhenClauses, 1)

	cond, ok := ce.WhenClauses[0].Condition.(*ast.BinaryExpression)
	require.True(t, ok)
	assert.Equal(t, "=", cond.Operator)
}

func TestDeriveFillReplace_ReplaceAndFill_BuildsReplaceThenFill(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "status", SQLType: "varchar"}}}
	_, stmt, err := DeriveFillReplace(base, "people_fill_replace", []FillReplaceRule{{Column: "status", ReplaceFrom: "N/A", ReplaceTo: nil, FillNullWith: "unknown"}})
	assert.Error(t, err)
	assert.Nil(t, stmt)

	_, stmt, err = DeriveFillReplace(base, "people_fill_replace", []FillReplaceRule{{Column: "status", ReplaceFrom: "N/A", ReplaceTo: "", FillNullWith: "unknown"}})
	require.NoError(t, err)

	query := stmt.Query.(*ast.SelectStatement)
	fn, ok := query.Columns[0].(*ast.FunctionCall)
	require.True(t, ok)
	assert.Equal(t, "COALESCE", fn.Name)

	_, ok = fn.Arguments[0].(*ast.CaseExpression)
	require.True(t, ok)
}

func TestDeriveFillReplace_InvalidInputs(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}
	tests := []struct {
		name  string
		base  TableSpec
		tbl   string
		rules []FillReplaceRule
	}{
		{name: "base required", base: TableSpec{}, tbl: "x", rules: []FillReplaceRule{{Column: "name", FillNullWith: "x"}}},
		{name: "derived required", base: base, tbl: "", rules: []FillReplaceRule{{Column: "name", FillNullWith: "x"}}},
		{name: "rules required", base: base, tbl: "x", rules: nil},
		{name: "column required", base: base, tbl: "x", rules: []FillReplaceRule{{Column: "", FillNullWith: "x"}}},
		{name: "column missing", base: base, tbl: "x", rules: []FillReplaceRule{{Column: "unknown", FillNullWith: "x"}}},
		{name: "duplicate rules", base: base, tbl: "x", rules: []FillReplaceRule{{Column: "name", FillNullWith: "x"}, {Column: "Name", FillNullWith: "y"}}},
		{name: "empty operation", base: base, tbl: "x", rules: []FillReplaceRule{{Column: "name"}}},
		{name: "partial replace from", base: base, tbl: "x", rules: []FillReplaceRule{{Column: "name", ReplaceFrom: "a"}}},
		{name: "partial replace to", base: base, tbl: "x", rules: []FillReplaceRule{{Column: "name", ReplaceTo: "b"}}},
		{name: "unsupported fill", base: base, tbl: "x", rules: []FillReplaceRule{{Column: "name", FillNullWith: struct{}{}}}},
		{name: "unsupported replace from", base: base, tbl: "x", rules: []FillReplaceRule{{Column: "name", ReplaceFrom: struct{}{}, ReplaceTo: "b"}}},
		{name: "unsupported replace to", base: base, tbl: "x", rules: []FillReplaceRule{{Column: "name", ReplaceFrom: "a", ReplaceTo: struct{}{}}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := DeriveFillReplace(tc.base, tc.tbl, tc.rules)
			assert.Error(t, err)
		})
	}
}

func TestDeriveFillReplace_DuckDBBackfill_FillReplaceCombined(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}, {Name: "status", SQLType: "varchar"}}}

	createBase, err := BuildCreateTable(base)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createBase, ast.CompactStyle()))
	require.NoError(t, err)

	insertBase, err := BuildInsert(InsertSpec{
		Table:   "people",
		Columns: []string{"id", "name", "status"},
		Rows: []map[string]any{
			{"id": 1, "name": nil, "status": "N/A"},
			{"id": 2, "name": "John", "status": "ok"},
			{"id": 3, "name": nil, "status": nil},
		},
	})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insertBase, ast.CompactStyle()))
	require.NoError(t, err)

	resolved, backfill, err := DeriveFillReplace(base, "people_filled", []FillReplaceRule{
		{Column: "name", FillNullWith: "unknown"},
		{Column: "status", ReplaceFrom: "N/A", ReplaceTo: nil, FillNullWith: "unknown"},
	})
	assert.Error(t, err)
	assert.Nil(t, backfill)
	assert.Equal(t, ResolvedBranch{}, resolved)

	resolved, backfill, err = DeriveFillReplace(base, "people_filled", []FillReplaceRule{
		{Column: "name", FillNullWith: "unknown"},
		{Column: "status", ReplaceFrom: "N/A", ReplaceTo: "", FillNullWith: "unknown"},
	})
	require.NoError(t, err)

	createDerived, err := BuildCreateTable(resolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createDerived, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
	require.NoError(t, err)

	rows, err := db.Query("SELECT id, name, status FROM people_filled ORDER BY id")
	require.NoError(t, err)
	defer rows.Close()

	var got []string
	for rows.Next() {
		var id int
		var name string
		var status string
		err := rows.Scan(&id, &name, &status)
		require.NoError(t, err)
		got = append(got, name+":"+status)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"unknown:", "John:ok", "unknown:unknown"}, got)
}
