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

func TestDeriveDeduplicate_ValidSpec_BuildsDistinctOnAndOrderBy(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "email", SQLType: "varchar"}}}
	resolved, stmt, err := DeriveDeduplicate(base, "people_dedup", DeduplicateSpec{
		Columns: []string{"email"},
		Keep:    DeduplicateKeepFirst,
		OrderBy: []SortKey{{Column: "id", Direction: SortDirectionAsc}},
	})
	require.NoError(t, err)
	require.NotNil(t, stmt)
	assert.Equal(t, "people_dedup", resolved.Table.Name)
	require.Len(t, resolved.Table.Columns, 2)

	query := stmt.Query.(*ast.SelectStatement)
	require.Len(t, query.DistinctOnColumns, 1)
	require.Len(t, query.OrderBy, 2)
}

func TestDeriveDeduplicate_KeepLast_InvertsOrder(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "email", SQLType: "varchar"}}}
	_, stmt, err := DeriveDeduplicate(base, "people_dedup", DeduplicateSpec{
		Columns: []string{"email"},
		Keep:    DeduplicateKeepLast,
		OrderBy: []SortKey{{Column: "id", Direction: SortDirectionAsc}},
	})
	require.NoError(t, err)

	query := stmt.Query.(*ast.SelectStatement)
	require.Len(t, query.OrderBy, 2)
	assert.False(t, query.OrderBy[1].Ascending)
}

func TestDeriveDeduplicate_InvalidInputs(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "email", SQLType: "varchar"}}}
	nulls := NullsOrder("middle")

	tests := []struct {
		name string
		base TableSpec
		tbl  string
		spec DeduplicateSpec
	}{
		{name: "base required", base: TableSpec{}, tbl: "x", spec: DeduplicateSpec{Columns: []string{"email"}, OrderBy: []SortKey{{Column: "id"}}}},
		{name: "table required", base: base, tbl: "", spec: DeduplicateSpec{Columns: []string{"email"}, OrderBy: []SortKey{{Column: "id"}}}},
		{name: "columns required", base: base, tbl: "x", spec: DeduplicateSpec{OrderBy: []SortKey{{Column: "id"}}}},
		{name: "orderby required", base: base, tbl: "x", spec: DeduplicateSpec{Columns: []string{"email"}}},
		{name: "dedup col required", base: base, tbl: "x", spec: DeduplicateSpec{Columns: []string{""}, OrderBy: []SortKey{{Column: "id"}}}},
		{name: "dedup col missing", base: base, tbl: "x", spec: DeduplicateSpec{Columns: []string{"unknown"}, OrderBy: []SortKey{{Column: "id"}}}},
		{name: "duplicate dedup col", base: base, tbl: "x", spec: DeduplicateSpec{Columns: []string{"email", "EMAIL"}, OrderBy: []SortKey{{Column: "id"}}}},
		{name: "invalid keep", base: base, tbl: "x", spec: DeduplicateSpec{Columns: []string{"email"}, Keep: DeduplicateKeep("middle"), OrderBy: []SortKey{{Column: "id"}}}},
		{name: "order col required", base: base, tbl: "x", spec: DeduplicateSpec{Columns: []string{"email"}, OrderBy: []SortKey{{Column: ""}}}},
		{name: "order col missing", base: base, tbl: "x", spec: DeduplicateSpec{Columns: []string{"email"}, OrderBy: []SortKey{{Column: "unknown"}}}},
		{name: "duplicate order col", base: base, tbl: "x", spec: DeduplicateSpec{Columns: []string{"email"}, OrderBy: []SortKey{{Column: "id"}, {Column: "ID"}}}},
		{name: "invalid order direction", base: base, tbl: "x", spec: DeduplicateSpec{Columns: []string{"email"}, OrderBy: []SortKey{{Column: "id", Direction: SortDirection("up")}}}},
		{name: "invalid nulls", base: base, tbl: "x", spec: DeduplicateSpec{Columns: []string{"email"}, OrderBy: []SortKey{{Column: "id", Nulls: &nulls}}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := DeriveDeduplicate(tc.base, tc.tbl, tc.spec)
			assert.Error(t, err)
		})
	}
}

func TestDeriveDeduplicate_DuckDBBackfill_FirstAndLast(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "email", SQLType: "varchar"}, {Name: "name", SQLType: "varchar"}}}

	createBase, err := BuildCreateTable(base)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createBase, ast.CompactStyle()))
	require.NoError(t, err)

	insertBase, err := BuildInsert(InsertSpec{
		Table:   "people",
		Columns: []string{"id", "email", "name"},
		Rows: []map[string]any{
			{"id": 1, "email": "a@x", "name": "A1"},
			{"id": 3, "email": "a@x", "name": "A3"},
			{"id": 2, "email": "b@x", "name": "B2"},
			{"id": 4, "email": "b@x", "name": "B4"},
		},
	})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insertBase, ast.CompactStyle()))
	require.NoError(t, err)

	firstResolved, firstBackfill, err := DeriveDeduplicate(base, "people_dedup_first", DeduplicateSpec{
		Columns: []string{"email"},
		Keep:    DeduplicateKeepFirst,
		OrderBy: []SortKey{{Column: "id", Direction: SortDirectionAsc}},
	})
	require.NoError(t, err)
	createFirst, err := BuildCreateTable(firstResolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createFirst, ast.CompactStyle()))
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(firstBackfill, ast.CompactStyle()))
	require.NoError(t, err)

	lastResolved, lastBackfill, err := DeriveDeduplicate(base, "people_dedup_last", DeduplicateSpec{
		Columns: []string{"email"},
		Keep:    DeduplicateKeepLast,
		OrderBy: []SortKey{{Column: "id", Direction: SortDirectionAsc}},
	})
	require.NoError(t, err)
	createLast, err := BuildCreateTable(lastResolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createLast, ast.CompactStyle()))
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(lastBackfill, ast.CompactStyle()))
	require.NoError(t, err)

	rowsFirst, err := db.Query("SELECT id, email FROM people_dedup_first ORDER BY email")
	require.NoError(t, err)
	defer rowsFirst.Close()
	var gotFirst []int
	for rowsFirst.Next() {
		var id int
		var email string
		err := rowsFirst.Scan(&id, &email)
		require.NoError(t, err)
		gotFirst = append(gotFirst, id)
	}
	require.NoError(t, rowsFirst.Err())
	assert.Equal(t, []int{1, 2}, gotFirst)

	rowsLast, err := db.Query("SELECT id, email FROM people_dedup_last ORDER BY email")
	require.NoError(t, err)
	defer rowsLast.Close()
	var gotLast []int
	for rowsLast.Next() {
		var id int
		var email string
		err := rowsLast.Scan(&id, &email)
		require.NoError(t, err)
		gotLast = append(gotLast, id)
	}
	require.NoError(t, rowsLast.Err())
	assert.Equal(t, []int{3, 4}, gotLast)
}
