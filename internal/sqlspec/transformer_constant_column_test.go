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

func TestDeriveConstantColumn_Valid_BuildsResolvedAndInsert(t *testing.T) {
	t.Parallel()

	base := TableSpec{
		Name: "people",
		Columns: []ColumnSpec{
			{Name: "id", SQLType: "integer"},
			{Name: "name", SQLType: "varchar"},
		},
	}

	resolved, stmt, err := DeriveConstantColumn(base, "people_constant", ColumnSpec{Name: "country", SQLType: "varchar"}, "BR")
	require.NoError(t, err)
	require.NotNil(t, stmt)

	assert.Equal(t, "people_constant", resolved.Table.Name)
	require.Len(t, resolved.Table.Columns, 3)
	assert.Equal(t, "country", resolved.Table.Columns[2].Name)
	assert.Equal(t, "varchar", resolved.Table.Columns[2].SQLType)
	assert.Equal(t, "BR", resolved.StaticValues["country"])

	query, ok := stmt.Query.(*ast.SelectStatement)
	require.True(t, ok)
	require.Len(t, query.Columns, 3)

	col0, ok := query.Columns[0].(*ast.Identifier)
	require.True(t, ok)
	assert.Equal(t, "id", col0.Name)

	col1, ok := query.Columns[1].(*ast.Identifier)
	require.True(t, ok)
	assert.Equal(t, "name", col1.Name)

	lit2, ok := query.Columns[2].(*ast.LiteralValue)
	require.True(t, ok)
	assert.Equal(t, "BR", lit2.Value)
	assert.Equal(t, "STRING", lit2.Type)
}

func TestDeriveConstantColumn_InvalidInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		base   TableSpec
		tbl    string
		column ColumnSpec
		value  any
	}{
		{name: "base required", base: TableSpec{}, tbl: "x", column: ColumnSpec{Name: "country", SQLType: "varchar"}, value: "BR"},
		{name: "derived required", base: TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}, tbl: "", column: ColumnSpec{Name: "country", SQLType: "varchar"}, value: "BR"},
		{name: "column name required", base: TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}, tbl: "x", column: ColumnSpec{Name: "", SQLType: "varchar"}, value: "BR"},
		{name: "column type required", base: TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}, tbl: "x", column: ColumnSpec{Name: "country", SQLType: ""}, value: "BR"},
		{name: "unsupported value", base: TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}, tbl: "x", column: ColumnSpec{Name: "country", SQLType: "varchar"}, value: struct{}{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := DeriveConstantColumn(tc.base, tc.tbl, tc.column, tc.value)
			assert.Error(t, err)
		})
	}
}

func TestDeriveConstantColumn_DuplicateColumnError(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "country", SQLType: "varchar"}}}
	_, _, err := DeriveConstantColumn(base, "people_constant", ColumnSpec{Name: "Country", SQLType: "varchar"}, "BR")
	assert.Error(t, err)
}

func TestDeriveConstantColumn_DuckDBBackfillAddsConstantValue(t *testing.T) {
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
		},
	})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insertBase, ast.CompactStyle()))
	require.NoError(t, err)

	resolved, backfill, err := DeriveConstantColumn(base, "people_constant", ColumnSpec{Name: "country", SQLType: "varchar"}, "BR")
	require.NoError(t, err)

	createDerived, err := BuildCreateTable(resolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createDerived, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
	require.NoError(t, err)

	rows, err := db.Query("SELECT id, name, country FROM people_constant ORDER BY id")
	require.NoError(t, err)
	defer rows.Close()

	var got []string
	for rows.Next() {
		var id int
		var name string
		var country string
		err := rows.Scan(&id, &name, &country)
		require.NoError(t, err)
		got = append(got, name+":"+country)
	}

	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"John:BR", "Doe:BR"}, got)
}
