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

func TestDeriveAggregate_ValidCountStar(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "team", SQLType: "varchar"}, {Name: "id", SQLType: "integer"}}}
	resolved, stmt, err := DeriveAggregate(base, "people_agg", AggregateSpec{
		GroupBy: []string{"team"},
		Metrics: []AggregateMetric{{Function: AggregateFunctionCount, As: "cnt"}},
	})
	require.NoError(t, err)
	require.NotNil(t, stmt)

	require.Len(t, resolved.Table.Columns, 2)
	assert.Equal(t, "team", resolved.Table.Columns[0].Name)
	assert.Equal(t, "cnt", resolved.Table.Columns[1].Name)
	assert.Equal(t, "bigint", resolved.Table.Columns[1].SQLType)

	query := stmt.Query.(*ast.SelectStatement)
	require.Len(t, query.Columns, 2)
	require.Len(t, query.GroupBy, 1)

	fn, ok := query.Columns[1].(*ast.FunctionCall)
	require.True(t, ok)
	assert.Equal(t, "COUNT", fn.Name)
}

func TestDeriveAggregate_ValidMultipleMetrics(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "sales", Columns: []ColumnSpec{{Name: "region", SQLType: "varchar"}, {Name: "amount", SQLType: "double"}}}
	resolved, _, err := DeriveAggregate(base, "sales_agg", AggregateSpec{
		GroupBy: []string{"region"},
		Metrics: []AggregateMetric{
			{Function: AggregateFunctionSum, Column: "amount", As: "sum_amount"},
			{Function: AggregateFunctionAvg, Column: "amount", As: "avg_amount"},
			{Function: AggregateFunctionMin, Column: "amount", As: "min_amount"},
			{Function: AggregateFunctionMax, Column: "amount", As: "max_amount"},
		},
	})
	require.NoError(t, err)
	require.Len(t, resolved.Table.Columns, 5)
	assert.Equal(t, "double", resolved.Table.Columns[1].SQLType)
	assert.Equal(t, "double", resolved.Table.Columns[2].SQLType)
}

func TestDeriveAggregate_InvalidInputs(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "team", SQLType: "varchar"}, {Name: "amount", SQLType: "double"}}}
	tests := []struct {
		name string
		base TableSpec
		tbl  string
		spec AggregateSpec
	}{
		{name: "base required", base: TableSpec{}, tbl: "x", spec: AggregateSpec{Metrics: []AggregateMetric{{Function: AggregateFunctionCount, As: "cnt"}}}},
		{name: "table required", base: base, tbl: "", spec: AggregateSpec{Metrics: []AggregateMetric{{Function: AggregateFunctionCount, As: "cnt"}}}},
		{name: "metrics required", base: base, tbl: "x", spec: AggregateSpec{}},
		{name: "group missing column", base: base, tbl: "x", spec: AggregateSpec{GroupBy: []string{"x"}, Metrics: []AggregateMetric{{Function: AggregateFunctionCount, As: "cnt"}}}},
		{name: "group duplicate", base: base, tbl: "x", spec: AggregateSpec{GroupBy: []string{"team", "TEAM"}, Metrics: []AggregateMetric{{Function: AggregateFunctionCount, As: "cnt"}}}},
		{name: "metric alias required", base: base, tbl: "x", spec: AggregateSpec{Metrics: []AggregateMetric{{Function: AggregateFunctionCount, As: ""}}}},
		{name: "metric function required", base: base, tbl: "x", spec: AggregateSpec{Metrics: []AggregateMetric{{Function: AggregateFunction(""), As: "cnt"}}}},
		{name: "metric function invalid", base: base, tbl: "x", spec: AggregateSpec{Metrics: []AggregateMetric{{Function: AggregateFunction("median"), Column: "amount", As: "m"}}}},
		{name: "metric col required", base: base, tbl: "x", spec: AggregateSpec{Metrics: []AggregateMetric{{Function: AggregateFunctionSum, As: "s"}}}},
		{name: "metric col missing", base: base, tbl: "x", spec: AggregateSpec{Metrics: []AggregateMetric{{Function: AggregateFunctionSum, Column: "x", As: "s"}}}},
		{name: "alias duplicate", base: base, tbl: "x", spec: AggregateSpec{Metrics: []AggregateMetric{{Function: AggregateFunctionCount, As: "cnt"}, {Function: AggregateFunctionCount, As: "CNT"}}}},
		{name: "alias collides group", base: base, tbl: "x", spec: AggregateSpec{GroupBy: []string{"team"}, Metrics: []AggregateMetric{{Function: AggregateFunctionCount, As: "TEAM"}}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := DeriveAggregate(tc.base, tc.tbl, tc.spec)
			assert.Error(t, err)
		})
	}
}

func TestDeriveAggregate_DuckDBBackfillGroupedMetrics(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	base := TableSpec{Name: "sales", Columns: []ColumnSpec{{Name: "region", SQLType: "varchar"}, {Name: "amount", SQLType: "double"}}}
	createBase, err := BuildCreateTable(base)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createBase, ast.CompactStyle()))
	require.NoError(t, err)

	insertBase, err := BuildInsert(InsertSpec{
		Table:   "sales",
		Columns: []string{"region", "amount"},
		Rows: []map[string]any{
			{"region": "north", "amount": 10.0},
			{"region": "north", "amount": 20.0},
			{"region": "south", "amount": 5.0},
		},
	})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insertBase, ast.CompactStyle()))
	require.NoError(t, err)

	resolved, backfill, err := DeriveAggregate(base, "sales_agg", AggregateSpec{
		GroupBy: []string{"region"},
		Metrics: []AggregateMetric{
			{Function: AggregateFunctionCount, As: "cnt"},
			{Function: AggregateFunctionSum, Column: "amount", As: "sum_amount"},
			{Function: AggregateFunctionAvg, Column: "amount", As: "avg_amount"},
			{Function: AggregateFunctionMin, Column: "amount", As: "min_amount"},
			{Function: AggregateFunctionMax, Column: "amount", As: "max_amount"},
		},
	})
	require.NoError(t, err)

	createDerived, err := BuildCreateTable(resolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createDerived, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
	require.NoError(t, err)

	rows, err := db.Query("SELECT region, cnt, sum_amount, avg_amount, min_amount, max_amount FROM sales_agg ORDER BY region")
	require.NoError(t, err)
	defer rows.Close()

	var got []string
	for rows.Next() {
		var region string
		var cnt int64
		var sumAmount float64
		var avgAmount float64
		var minAmount float64
		var maxAmount float64
		err := rows.Scan(&region, &cnt, &sumAmount, &avgAmount, &minAmount, &maxAmount)
		require.NoError(t, err)
		got = append(got, region)
		if region == "north" {
			assert.Equal(t, int64(2), cnt)
			assert.InDelta(t, 30.0, sumAmount, 0.0001)
			assert.InDelta(t, 15.0, avgAmount, 0.0001)
			assert.InDelta(t, 10.0, minAmount, 0.0001)
			assert.InDelta(t, 20.0, maxAmount, 0.0001)
		}
		if region == "south" {
			assert.Equal(t, int64(1), cnt)
			assert.InDelta(t, 5.0, sumAmount, 0.0001)
			assert.InDelta(t, 5.0, avgAmount, 0.0001)
			assert.InDelta(t, 5.0, minAmount, 0.0001)
			assert.InDelta(t, 5.0, maxAmount, 0.0001)
		}
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"north", "south"}, got)
}
