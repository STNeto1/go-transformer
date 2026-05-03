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

func TestDerivePivot_ValidSumBuildsSchemaAndClause(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "sales_long", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "region", SQLType: "varchar"}, {Name: "amount", SQLType: "integer"}}}
	resolved, stmt, err := DerivePivot(base, "sales_wide", PivotSpec{
		GroupBy:     []string{"id"},
		PivotColumn: "region",
		ValueColumn: "amount",
		AggFn:       PivotAggregateSum,
		InValues:    []string{"north", "south"},
	})
	require.NoError(t, err)
	require.Len(t, resolved.Table.Columns, 3)
	assert.Equal(t, "id", resolved.Table.Columns[0].Name)
	assert.Equal(t, "north", resolved.Table.Columns[1].Name)

	query := stmt.Query.(*ast.SelectStatement)
	require.Len(t, query.From, 1)
	require.NotNil(t, query.From[0].Pivot)
	assert.Equal(t, "region", query.From[0].Pivot.PivotColumn)
	assert.Equal(t, []string{"north", "south"}, query.From[0].Pivot.InValues)
}

func TestDerivePivot_ValidCount(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "sales_long", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "region", SQLType: "varchar"}, {Name: "amount", SQLType: "integer"}}}
	resolved, _, err := DerivePivot(base, "sales_wide", PivotSpec{
		GroupBy:     []string{"id"},
		PivotColumn: "region",
		ValueColumn: "amount",
		AggFn:       PivotAggregateCount,
		InValues:    []string{"north", "south"},
	})
	require.NoError(t, err)
	assert.Equal(t, "bigint", resolved.Table.Columns[1].SQLType)
}

func TestDerivePivot_InvalidInputs(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "sales_long", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "region", SQLType: "varchar"}, {Name: "amount", SQLType: "integer"}}}
	tests := []struct {
		name string
		base TableSpec
		tbl  string
		spec PivotSpec
	}{
		{name: "base required", base: TableSpec{}, tbl: "x", spec: PivotSpec{GroupBy: []string{"id"}, PivotColumn: "region", ValueColumn: "amount", InValues: []string{"north"}}},
		{name: "table required", base: base, tbl: "", spec: PivotSpec{GroupBy: []string{"id"}, PivotColumn: "region", ValueColumn: "amount", InValues: []string{"north"}}},
		{name: "group required", base: base, tbl: "x", spec: PivotSpec{PivotColumn: "region", ValueColumn: "amount", InValues: []string{"north"}}},
		{name: "pivot required", base: base, tbl: "x", spec: PivotSpec{GroupBy: []string{"id"}, ValueColumn: "amount", InValues: []string{"north"}}},
		{name: "value required", base: base, tbl: "x", spec: PivotSpec{GroupBy: []string{"id"}, PivotColumn: "region", InValues: []string{"north"}}},
		{name: "in required", base: base, tbl: "x", spec: PivotSpec{GroupBy: []string{"id"}, PivotColumn: "region", ValueColumn: "amount"}},
		{name: "bad agg", base: base, tbl: "x", spec: PivotSpec{GroupBy: []string{"id"}, PivotColumn: "region", ValueColumn: "amount", AggFn: PivotAggregate("avg"), InValues: []string{"north"}}},
		{name: "group missing", base: base, tbl: "x", spec: PivotSpec{GroupBy: []string{"unknown"}, PivotColumn: "region", ValueColumn: "amount", InValues: []string{"north"}}},
		{name: "pivot missing", base: base, tbl: "x", spec: PivotSpec{GroupBy: []string{"id"}, PivotColumn: "unknown", ValueColumn: "amount", InValues: []string{"north"}}},
		{name: "value missing", base: base, tbl: "x", spec: PivotSpec{GroupBy: []string{"id"}, PivotColumn: "region", ValueColumn: "unknown", InValues: []string{"north"}}},
		{name: "duplicate group", base: base, tbl: "x", spec: PivotSpec{GroupBy: []string{"id", "ID"}, PivotColumn: "region", ValueColumn: "amount", InValues: []string{"north"}}},
		{name: "group overlaps pivot", base: base, tbl: "x", spec: PivotSpec{GroupBy: []string{"region"}, PivotColumn: "region", ValueColumn: "amount", InValues: []string{"north"}}},
		{name: "duplicate in", base: base, tbl: "x", spec: PivotSpec{GroupBy: []string{"id"}, PivotColumn: "region", ValueColumn: "amount", InValues: []string{"north", "NORTH"}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := DerivePivot(tc.base, tc.tbl, tc.spec)
			assert.Error(t, err)
		})
	}
}

func TestDerivePivot_DuckDBBackfillSumAndCount(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	base := TableSpec{Name: "sales_long", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "region", SQLType: "varchar"}, {Name: "amount", SQLType: "integer"}}}
	createBase, err := BuildCreateTable(base)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createBase, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec("INSERT INTO sales_long (id, region, amount) VALUES (1, 'north', 10), (1, 'north', 5), (1, 'south', 7), (2, 'south', 3)")
	require.NoError(t, err)

	resSum, backfillSum, err := DerivePivot(base, "sales_wide_sum", PivotSpec{GroupBy: []string{"id"}, PivotColumn: "region", ValueColumn: "amount", AggFn: PivotAggregateSum, InValues: []string{"north", "south"}})
	require.NoError(t, err)
	createSum, err := BuildCreateTable(resSum.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createSum, ast.CompactStyle()))
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(backfillSum, ast.CompactStyle()))
	require.NoError(t, err)

	resCount, backfillCount, err := DerivePivot(base, "sales_wide_count", PivotSpec{GroupBy: []string{"id"}, PivotColumn: "region", ValueColumn: "amount", AggFn: PivotAggregateCount, InValues: []string{"north", "south"}})
	require.NoError(t, err)
	createCount, err := BuildCreateTable(resCount.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createCount, ast.CompactStyle()))
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(backfillCount, ast.CompactStyle()))
	require.NoError(t, err)

	var id int
	var north, south int
	err = db.QueryRow("SELECT id, north, south FROM sales_wide_sum WHERE id = 1").Scan(&id, &north, &south)
	require.NoError(t, err)
	assert.Equal(t, 15, north)
	assert.Equal(t, 7, south)

	var northCnt, southCnt int64
	err = db.QueryRow("SELECT north, south FROM sales_wide_count WHERE id = 1").Scan(&northCnt, &southCnt)
	require.NoError(t, err)
	assert.Equal(t, int64(2), northCnt)
	assert.Equal(t, int64(1), southCnt)
}
