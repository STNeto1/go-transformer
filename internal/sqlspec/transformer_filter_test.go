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

func TestDeriveFilter_ValidNeFilter_BuildsWhere(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "name", SQLType: "varchar"}}}
	_, stmt, err := DeriveFilter(base, "people_filtered", FilterRow{Column: "name", Operation: FilterOperationNe, Value: "John"})
	require.NoError(t, err)

	query, ok := stmt.Query.(*ast.SelectStatement)
	require.True(t, ok)
	where, ok := query.Where.(*ast.BinaryExpression)
	require.True(t, ok)
	assert.Equal(t, "!=", where.Operator)
}

func TestDeriveFilter_StringPatternOps_BuildLikeWhere(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		op       FilterOperation
		value    string
		expected string
	}{
		{name: "contains", op: FilterOperationContains, value: "oh", expected: "%oh%"},
		{name: "startsWith", op: FilterOperationStartsWith, value: "Jo", expected: "Jo%"},
		{name: "endsWith", op: FilterOperationEndsWith, value: "hn", expected: "%hn"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "name", SQLType: "varchar"}}}
			_, stmt, err := DeriveFilter(base, "people_filtered", FilterRow{Column: "name", Operation: tc.op, Value: tc.value})
			require.NoError(t, err)

			query, ok := stmt.Query.(*ast.SelectStatement)
			require.True(t, ok)
			where, ok := query.Where.(*ast.BinaryExpression)
			require.True(t, ok)
			assert.Equal(t, "LIKE", where.Operator)

			right, ok := where.Right.(*ast.LiteralValue)
			require.True(t, ok)
			assert.Equal(t, tc.expected, right.Value)
		})
	}
}

func TestDeriveFilter_ValidGtLtFilter_BuildsWhere(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}

	_, gtStmt, err := DeriveFilter(base, "people_gt", FilterRow{Column: "id", Operation: FilterOperationGt, Value: 10})
	require.NoError(t, err)
	gtQuery := gtStmt.Query.(*ast.SelectStatement)
	gtWhere := gtQuery.Where.(*ast.BinaryExpression)
	assert.Equal(t, ">", gtWhere.Operator)

	_, ltStmt, err := DeriveFilter(base, "people_lt", FilterRow{Column: "id", Operation: FilterOperationLt, Value: 10})
	require.NoError(t, err)
	ltQuery := ltStmt.Query.(*ast.SelectStatement)
	ltWhere := ltQuery.Where.(*ast.BinaryExpression)
	assert.Equal(t, "<", ltWhere.Operator)
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

func TestDeriveFilter_StringOperationsRequireStringValue(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "name", SQLType: "varchar"}}}
	_, _, err := DeriveFilter(base, "people_filtered", FilterRow{
		Column:    "name",
		Operation: FilterOperationContains,
		Value:     42,
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

func TestDeriveFilter_DuckDBBackfill_NE_GT_LT(t *testing.T) {
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

	insertBase, err := BuildInsert(InsertSpec{Table: "people", Columns: []string{"id", "name"}, Rows: []map[string]any{{"id": 1, "name": "John"}, {"id": 2, "name": "Doe"}, {"id": 3, "name": "Mary"}}})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insertBase, ast.CompactStyle()))
	require.NoError(t, err)

	tests := []struct {
		name        string
		table       string
		filter      FilterRow
		expectedIDs []int
	}{
		{name: "ne", table: "people_ne", filter: FilterRow{Column: "name", Operation: FilterOperationNe, Value: "John"}, expectedIDs: []int{2, 3}},
		{name: "gt", table: "people_gt", filter: FilterRow{Column: "id", Operation: FilterOperationGt, Value: 1}, expectedIDs: []int{2, 3}},
		{name: "lt", table: "people_lt", filter: FilterRow{Column: "id", Operation: FilterOperationLt, Value: 3}, expectedIDs: []int{1, 2}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			resolved, backfill, err := DeriveFilter(base, tc.table, tc.filter)
			require.NoError(t, err)

			createDerived, err := BuildCreateTable(resolved.Table)
			require.NoError(t, err)
			_, err = db.Exec(formatter.FormatStatement(createDerived, ast.CompactStyle()))
			require.NoError(t, err)

			_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
			require.NoError(t, err)

			rows, err := db.Query("SELECT id FROM " + tc.table + " ORDER BY id")
			require.NoError(t, err)
			defer rows.Close()

			var gotIDs []int
			for rows.Next() {
				var id int
				err := rows.Scan(&id)
				require.NoError(t, err)
				gotIDs = append(gotIDs, id)
			}
			require.NoError(t, rows.Err())
			assert.Equal(t, tc.expectedIDs, gotIDs)
		})
	}
}

func TestDeriveFilter_DuckDBBackfill_StringOps(t *testing.T) {
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

	insertBase, err := BuildInsert(InsertSpec{Table: "people", Columns: []string{"id", "name"}, Rows: []map[string]any{{"id": 1, "name": "John"}, {"id": 2, "name": "Doe"}, {"id": 3, "name": "Mary"}, {"id": 4, "name": "Jane"}}})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insertBase, ast.CompactStyle()))
	require.NoError(t, err)

	tests := []struct {
		name        string
		table       string
		filter      FilterRow
		expectedIDs []int
	}{
		{name: "contains", table: "people_contains", filter: FilterRow{Column: "name", Operation: FilterOperationContains, Value: "ar"}, expectedIDs: []int{3}},
		{name: "startsWith", table: "people_starts", filter: FilterRow{Column: "name", Operation: FilterOperationStartsWith, Value: "Ja"}, expectedIDs: []int{4}},
		{name: "endsWith", table: "people_ends", filter: FilterRow{Column: "name", Operation: FilterOperationEndsWith, Value: "hn"}, expectedIDs: []int{1}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			resolved, backfill, err := DeriveFilter(base, tc.table, tc.filter)
			require.NoError(t, err)

			createDerived, err := BuildCreateTable(resolved.Table)
			require.NoError(t, err)
			_, err = db.Exec(formatter.FormatStatement(createDerived, ast.CompactStyle()))
			require.NoError(t, err)

			_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
			require.NoError(t, err)

			rows, err := db.Query("SELECT id FROM " + tc.table + " ORDER BY id")
			require.NoError(t, err)
			defer rows.Close()

			var gotIDs []int
			for rows.Next() {
				var id int
				err := rows.Scan(&id)
				require.NoError(t, err)
				gotIDs = append(gotIDs, id)
			}
			require.NoError(t, rows.Err())
			assert.Equal(t, tc.expectedIDs, gotIDs)
		})
	}
}
