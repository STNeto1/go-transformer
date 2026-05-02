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

func TestDeriveLimit_ValidLimit_BuildsLimit(t *testing.T) {
	t.Parallel()

	offset := 2
	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}

	resolved, stmt, err := DeriveLimit(base, "people_limited", LimitSpec{Count: 3, Offset: &offset})
	require.NoError(t, err)
	require.NotNil(t, stmt)
	assert.Equal(t, "people_limited", resolved.Table.Name)
	assert.Len(t, resolved.Table.Columns, 1)

	query, ok := stmt.Query.(*ast.SelectStatement)
	require.True(t, ok)
	require.NotNil(t, query.Limit)
	assert.Equal(t, 3, *query.Limit)
	require.NotNil(t, query.Offset)
	assert.Equal(t, 2, *query.Offset)
}

func TestDeriveLimit_InvalidInputs(t *testing.T) {
	t.Parallel()

	neg := -1
	tests := []struct {
		name string
		base TableSpec
		tbl  string
		spec LimitSpec
	}{
		{name: "base required", base: TableSpec{}, tbl: "x", spec: LimitSpec{Count: 1}},
		{name: "table required", base: TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}, tbl: "", spec: LimitSpec{Count: 1}},
		{name: "count required", base: TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}, tbl: "x", spec: LimitSpec{Count: 0}},
		{name: "offset non-negative", base: TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}, tbl: "x", spec: LimitSpec{Count: 1, Offset: &neg}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := DeriveLimit(tc.base, tc.tbl, tc.spec)
			assert.Error(t, err)
		})
	}
}

func TestDeriveLimit_DuckDBBackfillLimitsRows(t *testing.T) {
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
			{"id": 2, "name": "Carol"},
			{"id": 4, "name": "Dave"},
		},
	})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insertBase, ast.CompactStyle()))
	require.NoError(t, err)

	resolvedSort, sortBackfill, err := DeriveSort(base, "people_sorted_for_limit", []SortKey{{Column: "id", Direction: SortDirectionAsc}})
	require.NoError(t, err)
	createSorted, err := BuildCreateTable(resolvedSort.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createSorted, ast.CompactStyle()))
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(sortBackfill, ast.CompactStyle()))
	require.NoError(t, err)

	offset := 1
	resolvedLimit, limitBackfill, err := DeriveLimit(resolvedSort.Table, "people_limited", LimitSpec{Count: 2, Offset: &offset})
	require.NoError(t, err)
	createLimited, err := BuildCreateTable(resolvedLimit.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createLimited, ast.CompactStyle()))
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(limitBackfill, ast.CompactStyle()))
	require.NoError(t, err)

	rows, err := db.Query("SELECT id FROM people_limited ORDER BY id")
	require.NoError(t, err)
	defer rows.Close()

	var got []int
	for rows.Next() {
		var id int
		err := rows.Scan(&id)
		require.NoError(t, err)
		got = append(got, id)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []int{2, 3}, got)
}
