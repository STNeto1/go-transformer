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

func TestDeriveSort_ValidSort_BuildsOrderBy(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}

	resolved, stmt, err := DeriveSort(base, "people_sorted", []SortKey{{Column: "name", Direction: SortDirectionDesc}})
	require.NoError(t, err)
	require.NotNil(t, stmt)
	assert.Equal(t, "people_sorted", resolved.Table.Name)
	assert.Len(t, resolved.Table.Columns, 2)

	query, ok := stmt.Query.(*ast.SelectStatement)
	require.True(t, ok)
	require.Len(t, query.OrderBy, 1)

	col, ok := query.OrderBy[0].Expression.(*ast.Identifier)
	require.True(t, ok)
	assert.Equal(t, "name", col.Name)
	assert.False(t, query.OrderBy[0].Ascending)
	assert.Nil(t, query.OrderBy[0].NullsFirst)
}

func TestDeriveSort_CaseInsensitiveColumnMatch(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "Name", SQLType: "varchar"}}}
	_, stmt, err := DeriveSort(base, "people_sorted", []SortKey{{Column: "name"}})
	require.NoError(t, err)

	query, ok := stmt.Query.(*ast.SelectStatement)
	require.True(t, ok)
	col := query.OrderBy[0].Expression.(*ast.Identifier)
	assert.Equal(t, "Name", col.Name)
	assert.True(t, query.OrderBy[0].Ascending)
}

func TestDeriveSort_DuplicateColumnsError(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}
	_, _, err := DeriveSort(base, "people_sorted", []SortKey{{Column: "id"}, {Column: "ID"}})
	assert.Error(t, err)
}

func TestDeriveSort_InvalidInputs(t *testing.T) {
	t.Parallel()

	nulls := NullsOrder("middle")

	tests := []struct {
		name string
		base TableSpec
		tbl  string
		keys []SortKey
	}{
		{name: "base required", base: TableSpec{}, tbl: "x", keys: []SortKey{{Column: "id"}}},
		{name: "table required", base: TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}, tbl: "", keys: []SortKey{{Column: "id"}}},
		{name: "keys required", base: TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}, tbl: "x", keys: nil},
		{name: "column required", base: TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}, tbl: "x", keys: []SortKey{{Column: ""}}},
		{name: "unknown column", base: TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}, tbl: "x", keys: []SortKey{{Column: "name"}}},
		{name: "invalid direction", base: TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}, tbl: "x", keys: []SortKey{{Column: "id", Direction: SortDirection("up")}}},
		{name: "invalid nulls", base: TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}, tbl: "x", keys: []SortKey{{Column: "id", Nulls: &nulls}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := DeriveSort(tc.base, tc.tbl, tc.keys)
			assert.Error(t, err)
		})
	}
}

func TestDeriveSort_DuckDBBackfillSortsRows(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}

	createBase, err := BuildCreateTable(base)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createBase, ast.CompactStyle()))
	require.NoError(t, err)

	insertBase, err := BuildInsert(InsertSpec{
		Table:   "people",
		Columns: []string{"id", "name"},
		Rows: []map[string]any{
			{"id": 3, "name": "Bob"},
			{"id": 1, "name": "Alice"},
			{"id": 2, "name": "Alice"},
		},
	})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insertBase, ast.CompactStyle()))
	require.NoError(t, err)

	resolved, backfill, err := DeriveSort(base, "people_sorted", []SortKey{{Column: "name", Direction: SortDirectionAsc}, {Column: "id", Direction: SortDirectionDesc}})
	require.NoError(t, err)

	createDerived, err := BuildCreateTable(resolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createDerived, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
	require.NoError(t, err)

	rows, err := db.Query("SELECT id, name FROM people_sorted")
	require.NoError(t, err)
	defer rows.Close()

	var got []string
	for rows.Next() {
		var id int
		var name string
		err := rows.Scan(&id, &name)
		require.NoError(t, err)
		got = append(got, fmt.Sprintf("%s#%d", name, id))
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"Alice#2", "Alice#1", "Bob#3"}, got)
}
