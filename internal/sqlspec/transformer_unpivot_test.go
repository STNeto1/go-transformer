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

func TestDeriveUnpivot_ValidBuildsSchemaAndClause(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "sales", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "north", SQLType: "integer"}, {Name: "south", SQLType: "integer"}}}
	resolved, stmt, err := DeriveUnpivot(base, "sales_long", UnpivotSpec{
		Passthrough: []string{"id"},
		NameColumn:  ColumnSpec{Name: "region", SQLType: "varchar"},
		ValueColumn: ColumnSpec{Name: "amount", SQLType: "integer"},
		InColumns:   []string{"north", "south"},
	})
	require.NoError(t, err)
	require.Len(t, resolved.Table.Columns, 3)
	assert.Equal(t, "id", resolved.Table.Columns[0].Name)
	assert.Equal(t, "region", resolved.Table.Columns[1].Name)
	assert.Equal(t, "amount", resolved.Table.Columns[2].Name)

	query := stmt.Query.(*ast.SelectStatement)
	require.Len(t, query.From, 1)
	require.NotNil(t, query.From[0].Unpivot)
	assert.Equal(t, "region", query.From[0].Unpivot.NameColumn)
	assert.Equal(t, "amount", query.From[0].Unpivot.ValueColumn)
	assert.Equal(t, []string{"north", "south"}, query.From[0].Unpivot.InColumns)
}

func TestDeriveUnpivot_InvalidInputs(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "sales", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "north", SQLType: "integer"}, {Name: "south", SQLType: "integer"}}}
	tests := []struct {
		name string
		base TableSpec
		tbl  string
		spec UnpivotSpec
	}{
		{name: "base required", base: TableSpec{}, tbl: "x", spec: UnpivotSpec{InColumns: []string{"north"}, NameColumn: ColumnSpec{Name: "region", SQLType: "varchar"}, ValueColumn: ColumnSpec{Name: "amount", SQLType: "integer"}}},
		{name: "table required", base: base, tbl: "", spec: UnpivotSpec{InColumns: []string{"north"}, NameColumn: ColumnSpec{Name: "region", SQLType: "varchar"}, ValueColumn: ColumnSpec{Name: "amount", SQLType: "integer"}}},
		{name: "in required", base: base, tbl: "x", spec: UnpivotSpec{NameColumn: ColumnSpec{Name: "region", SQLType: "varchar"}, ValueColumn: ColumnSpec{Name: "amount", SQLType: "integer"}}},
		{name: "name col required", base: base, tbl: "x", spec: UnpivotSpec{InColumns: []string{"north"}, ValueColumn: ColumnSpec{Name: "amount", SQLType: "integer"}}},
		{name: "value col required", base: base, tbl: "x", spec: UnpivotSpec{InColumns: []string{"north"}, NameColumn: ColumnSpec{Name: "region", SQLType: "varchar"}}},
		{name: "same output names", base: base, tbl: "x", spec: UnpivotSpec{InColumns: []string{"north"}, NameColumn: ColumnSpec{Name: "x", SQLType: "varchar"}, ValueColumn: ColumnSpec{Name: "X", SQLType: "integer"}}},
		{name: "passthrough missing", base: base, tbl: "x", spec: UnpivotSpec{Passthrough: []string{"unknown"}, InColumns: []string{"north"}, NameColumn: ColumnSpec{Name: "region", SQLType: "varchar"}, ValueColumn: ColumnSpec{Name: "amount", SQLType: "integer"}}},
		{name: "in missing", base: base, tbl: "x", spec: UnpivotSpec{InColumns: []string{"unknown"}, NameColumn: ColumnSpec{Name: "region", SQLType: "varchar"}, ValueColumn: ColumnSpec{Name: "amount", SQLType: "integer"}}},
		{name: "overlap", base: base, tbl: "x", spec: UnpivotSpec{Passthrough: []string{"north"}, InColumns: []string{"north"}, NameColumn: ColumnSpec{Name: "region", SQLType: "varchar"}, ValueColumn: ColumnSpec{Name: "amount", SQLType: "integer"}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := DeriveUnpivot(tc.base, tc.tbl, tc.spec)
			assert.Error(t, err)
		})
	}
}

func TestDeriveUnpivot_DuckDBBackfill(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	base := TableSpec{Name: "sales", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "north", SQLType: "integer"}, {Name: "south", SQLType: "integer"}}}
	createBase, err := BuildCreateTable(base)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createBase, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec("INSERT INTO sales (id, north, south) VALUES (1, 10, 20), (2, 30, 40)")
	require.NoError(t, err)

	resolved, backfill, err := DeriveUnpivot(base, "sales_long", UnpivotSpec{
		Passthrough: []string{"id"},
		NameColumn:  ColumnSpec{Name: "region", SQLType: "varchar"},
		ValueColumn: ColumnSpec{Name: "amount", SQLType: "integer"},
		InColumns:   []string{"north", "south"},
	})
	require.NoError(t, err)

	createDerived, err := BuildCreateTable(resolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createDerived, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
	require.NoError(t, err)

	rows, err := db.Query("SELECT id, region, amount FROM sales_long ORDER BY id, region")
	require.NoError(t, err)
	defer rows.Close()

	var got []string
	for rows.Next() {
		var id int
		var region string
		var amount int
		err := rows.Scan(&id, &region, &amount)
		require.NoError(t, err)
		got = append(got, region)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"north", "south", "north", "south"}, got)
}
