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

func TestDeriveJoinInner_ValidJoinBuildsInsertSelect(t *testing.T) {
	t.Parallel()

	left := TableSpec{Name: "orders", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "customer_id", SQLType: "integer"}}}
	right := TableSpec{Name: "customers", Columns: []ColumnSpec{{Name: "customer_id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}

	resolved, stmt, err := DeriveJoinInner(left, right, "orders_joined", []JoinCondition{{LeftColumn: "customer_id", RightColumn: "customer_id"}})
	require.NoError(t, err)
	require.NotNil(t, stmt)
	require.Len(t, resolved.Table.Columns, 4)

	query, ok := stmt.Query.(*ast.SelectStatement)
	require.True(t, ok)
	require.Len(t, query.Joins, 1)
	assert.Equal(t, "INNER", query.Joins[0].Type)
}

func TestDeriveJoinInner_InvalidInputs(t *testing.T) {
	t.Parallel()

	left := TableSpec{Name: "orders", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "customer_id", SQLType: "integer"}}}
	right := TableSpec{Name: "customers", Columns: []ColumnSpec{{Name: "customer_id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}

	tests := []struct {
		name   string
		left   TableSpec
		right  TableSpec
		table  string
		on     []JoinCondition
		hasErr bool
	}{
		{name: "left required", left: TableSpec{}, right: right, table: "x", on: []JoinCondition{{LeftColumn: "customer_id", RightColumn: "customer_id"}}, hasErr: true},
		{name: "right required", left: left, right: TableSpec{}, table: "x", on: []JoinCondition{{LeftColumn: "customer_id", RightColumn: "customer_id"}}, hasErr: true},
		{name: "table required", left: left, right: right, table: "", on: []JoinCondition{{LeftColumn: "customer_id", RightColumn: "customer_id"}}, hasErr: true},
		{name: "on required", left: left, right: right, table: "x", on: nil, hasErr: true},
		{name: "join left column missing", left: left, right: right, table: "x", on: []JoinCondition{{LeftColumn: "missing", RightColumn: "customer_id"}}, hasErr: true},
		{name: "join right column missing", left: left, right: right, table: "x", on: []JoinCondition{{LeftColumn: "customer_id", RightColumn: "missing"}}, hasErr: true},
		{name: "duplicate condition", left: left, right: right, table: "x", on: []JoinCondition{{LeftColumn: "customer_id", RightColumn: "customer_id"}, {LeftColumn: "CUSTOMER_ID", RightColumn: "customer_id"}}, hasErr: true},
		{name: "duplicate output columns handled", left: left, right: TableSpec{Name: "customers", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}, table: "x", on: []JoinCondition{{LeftColumn: "id", RightColumn: "id"}}, hasErr: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := DeriveJoinInner(tc.left, tc.right, tc.table, tc.on)
			if tc.hasErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDeriveJoinInner_DuckDBBackfillInnerJoin(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	left := TableSpec{Name: "orders", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "customer_id", SQLType: "integer"}}}
	right := TableSpec{Name: "customers", Columns: []ColumnSpec{{Name: "customer_id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}

	createLeft, err := BuildCreateTable(left)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createLeft, ast.CompactStyle()))
	require.NoError(t, err)

	createRight, err := BuildCreateTable(right)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createRight, ast.CompactStyle()))
	require.NoError(t, err)

	insertLeft, err := BuildInsert(InsertSpec{Table: "orders", Columns: []string{"id", "customer_id"}, Rows: []map[string]any{{"id": 1, "customer_id": 10}, {"id": 2, "customer_id": 20}, {"id": 3, "customer_id": 30}}})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insertLeft, ast.CompactStyle()))
	require.NoError(t, err)

	insertRight, err := BuildInsert(InsertSpec{Table: "customers", Columns: []string{"customer_id", "name"}, Rows: []map[string]any{{"customer_id": 10, "name": "Alice"}, {"customer_id": 20, "name": "Bob"}}})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insertRight, ast.CompactStyle()))
	require.NoError(t, err)

	resolved, backfill, err := DeriveJoinInner(left, right, "orders_joined", []JoinCondition{{LeftColumn: "customer_id", RightColumn: "customer_id"}})
	require.NoError(t, err)

	createDerived, err := BuildCreateTable(resolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createDerived, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
	require.NoError(t, err)

	rows, err := db.Query("SELECT id, customer_id, customer_id_right, name FROM orders_joined ORDER BY id")
	require.NoError(t, err)
	defer rows.Close()

	var got []string
	for rows.Next() {
		var id int
		var customerID int
		var customerIDRight int
		var name string
		err := rows.Scan(&id, &customerID, &customerIDRight, &name)
		require.NoError(t, err)
		assert.Equal(t, customerID, customerIDRight)
		got = append(got, name)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"Alice", "Bob"}, got)
}

func TestDeriveJoin_ValidExplicitProjectionAndLeftJoin(t *testing.T) {
	t.Parallel()

	left := TableSpec{Name: "orders", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "customer_id", SQLType: "integer"}}}
	right := TableSpec{Name: "customers", Columns: []ColumnSpec{{Name: "customer_id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}

	resolved, stmt, err := DeriveJoin(left, right, "orders_joined", JoinSpec{
		Type: JoinTypeLeft,
		On:   []JoinCondition{{LeftColumn: "customer_id", RightColumn: "customer_id"}},
		Select: []JoinSelectColumn{
			{Side: "left", Column: "id", As: "order_id"},
			{Side: "right", Column: "name", As: "customer_name"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, stmt)
	require.Len(t, resolved.Table.Columns, 2)
	assert.Equal(t, "order_id", resolved.Table.Columns[0].Name)
	assert.Equal(t, "customer_name", resolved.Table.Columns[1].Name)

	query := stmt.Query.(*ast.SelectStatement)
	require.Len(t, query.Joins, 1)
	assert.Equal(t, "LEFT", query.Joins[0].Type)
}

func TestDeriveJoin_InvalidSpecInputs(t *testing.T) {
	t.Parallel()

	left := TableSpec{Name: "orders", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "customer_id", SQLType: "integer"}}}
	right := TableSpec{Name: "customers", Columns: []ColumnSpec{{Name: "customer_id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}

	tests := []struct {
		name string
		spec JoinSpec
	}{
		{name: "select required", spec: JoinSpec{On: []JoinCondition{{LeftColumn: "customer_id", RightColumn: "customer_id"}}}},
		{name: "invalid join type", spec: JoinSpec{Type: JoinType("outer"), On: []JoinCondition{{LeftColumn: "customer_id", RightColumn: "customer_id"}}, Select: []JoinSelectColumn{{Side: "left", Column: "id", As: "id"}}}},
		{name: "projection side invalid", spec: JoinSpec{On: []JoinCondition{{LeftColumn: "customer_id", RightColumn: "customer_id"}}, Select: []JoinSelectColumn{{Side: "middle", Column: "id", As: "id"}}}},
		{name: "projection column missing", spec: JoinSpec{On: []JoinCondition{{LeftColumn: "customer_id", RightColumn: "customer_id"}}, Select: []JoinSelectColumn{{Side: "right", Column: "unknown", As: "x"}}}},
		{name: "duplicate aliases", spec: JoinSpec{On: []JoinCondition{{LeftColumn: "customer_id", RightColumn: "customer_id"}}, Select: []JoinSelectColumn{{Side: "left", Column: "id", As: "id"}, {Side: "right", Column: "name", As: "ID"}}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := DeriveJoin(left, right, "orders_joined", tc.spec)
			assert.Error(t, err)
		})
	}
}

func TestDeriveJoin_DuckDBBackfillLeftJoinKeepsUnmatched(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	left := TableSpec{Name: "orders", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "customer_id", SQLType: "integer"}}}
	right := TableSpec{Name: "customers", Columns: []ColumnSpec{{Name: "customer_id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}

	createLeft, err := BuildCreateTable(left)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createLeft, ast.CompactStyle()))
	require.NoError(t, err)

	createRight, err := BuildCreateTable(right)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createRight, ast.CompactStyle()))
	require.NoError(t, err)

	insertLeft, err := BuildInsert(InsertSpec{Table: "orders", Columns: []string{"id", "customer_id"}, Rows: []map[string]any{{"id": 1, "customer_id": 10}, {"id": 2, "customer_id": 20}, {"id": 3, "customer_id": 30}}})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insertLeft, ast.CompactStyle()))
	require.NoError(t, err)

	insertRight, err := BuildInsert(InsertSpec{Table: "customers", Columns: []string{"customer_id", "name"}, Rows: []map[string]any{{"customer_id": 10, "name": "Alice"}, {"customer_id": 20, "name": "Bob"}}})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insertRight, ast.CompactStyle()))
	require.NoError(t, err)

	resolved, backfill, err := DeriveJoin(left, right, "orders_left_joined", JoinSpec{
		Type: JoinTypeLeft,
		On:   []JoinCondition{{LeftColumn: "customer_id", RightColumn: "customer_id"}},
		Select: []JoinSelectColumn{
			{Side: "left", Column: "id", As: "order_id"},
			{Side: "left", Column: "customer_id", As: "customer_id"},
			{Side: "right", Column: "name", As: "customer_name"},
		},
	})
	require.NoError(t, err)

	createDerived, err := BuildCreateTable(resolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createDerived, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
	require.NoError(t, err)

	rows, err := db.Query("SELECT order_id, customer_name FROM orders_left_joined ORDER BY order_id")
	require.NoError(t, err)
	defer rows.Close()

	var got []string
	for rows.Next() {
		var orderID int
		var customerName sql.NullString
		err := rows.Scan(&orderID, &customerName)
		require.NoError(t, err)
		if customerName.Valid {
			got = append(got, customerName.String)
		} else {
			got = append(got, "<null>")
		}
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"Alice", "Bob", "<null>"}, got)
}
