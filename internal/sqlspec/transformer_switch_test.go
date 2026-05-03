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

func TestDeriveSwitch_DefaultModeAll(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}
	res, err := DeriveSwitch(base, SwitchSpec{
		Branches: []SwitchBranchSpec{{
			Label:     "match",
			TableName: "people_match",
			Rules: []FilterRow{
				{Column: "id", Operation: FilterOperationGt, Value: 1},
				{Column: "name", Operation: FilterOperationEq, Value: "John"},
			},
		}},
		DefaultLabel:     "default",
		DefaultTableName: "people_default",
	})
	require.NoError(t, err)

	ifQuery := res.Backfills["match"].Query.(*ast.SelectStatement)
	where, ok := ifQuery.Where.(*ast.BinaryExpression)
	require.True(t, ok)
	assert.Equal(t, "AND", where.Operator)
}

func TestDeriveSwitch_EmptyRulesBranchIsMatchAll(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}
	res, err := DeriveSwitch(base, SwitchSpec{
		Branches:         []SwitchBranchSpec{{Label: "all", TableName: "people_all"}},
		DefaultLabel:     "default",
		DefaultTableName: "people_default",
	})
	require.NoError(t, err)

	allQuery := res.Backfills["all"].Query.(*ast.SelectStatement)
	assert.Nil(t, allQuery.Where)

	defQuery := res.DefaultBackfill.Query.(*ast.SelectStatement)
	where, ok := defQuery.Where.(*ast.BinaryExpression)
	require.True(t, ok)
	assert.Equal(t, "=", where.Operator)
}

func TestDeriveSwitch_InvalidInputs(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}
	tests := []struct {
		name string
		base TableSpec
		spec SwitchSpec
	}{
		{name: "base required", base: TableSpec{}, spec: SwitchSpec{Branches: []SwitchBranchSpec{{Label: "a", TableName: "t1"}}, DefaultLabel: "d", DefaultTableName: "td"}},
		{name: "branches required", base: base, spec: SwitchSpec{DefaultLabel: "d", DefaultTableName: "td"}},
		{name: "default label required", base: base, spec: SwitchSpec{Branches: []SwitchBranchSpec{{Label: "a", TableName: "t1"}}, DefaultTableName: "td"}},
		{name: "default table required", base: base, spec: SwitchSpec{Branches: []SwitchBranchSpec{{Label: "a", TableName: "t1"}}, DefaultLabel: "d"}},
		{name: "branch label required", base: base, spec: SwitchSpec{Branches: []SwitchBranchSpec{{Label: "", TableName: "t1"}}, DefaultLabel: "d", DefaultTableName: "td"}},
		{name: "branch table required", base: base, spec: SwitchSpec{Branches: []SwitchBranchSpec{{Label: "a", TableName: ""}}, DefaultLabel: "d", DefaultTableName: "td"}},
		{name: "duplicate label", base: base, spec: SwitchSpec{Branches: []SwitchBranchSpec{{Label: "a", TableName: "t1"}, {Label: "A", TableName: "t2"}}, DefaultLabel: "d", DefaultTableName: "td"}},
		{name: "duplicate table", base: base, spec: SwitchSpec{Branches: []SwitchBranchSpec{{Label: "a", TableName: "t1"}, {Label: "b", TableName: "T1"}}, DefaultLabel: "d", DefaultTableName: "td"}},
		{name: "default label collides", base: base, spec: SwitchSpec{Branches: []SwitchBranchSpec{{Label: "a", TableName: "t1"}}, DefaultLabel: "A", DefaultTableName: "td"}},
		{name: "default table collides", base: base, spec: SwitchSpec{Branches: []SwitchBranchSpec{{Label: "a", TableName: "t1"}}, DefaultLabel: "d", DefaultTableName: "T1"}},
		{name: "invalid mode", base: base, spec: SwitchSpec{Branches: []SwitchBranchSpec{{Label: "a", TableName: "t1", Mode: ConditionalMode("weird")}}, DefaultLabel: "d", DefaultTableName: "td"}},
		{name: "invalid rule", base: base, spec: SwitchSpec{Branches: []SwitchBranchSpec{{Label: "a", TableName: "t1", Rules: []FilterRow{{Column: "unknown", Operation: FilterOperationEq, Value: 1}}}}, DefaultLabel: "d", DefaultTableName: "td"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := DeriveSwitch(tc.base, tc.spec)
			assert.Error(t, err)
		})
	}
}

func TestDeriveSwitch_DuckDBIndependentMultiMatchAndDefault(t *testing.T) {
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
			{"id": 1, "name": "John"},
			{"id": 2, "name": "John"},
			{"id": 3, "name": "Doe"},
			{"id": 4, "name": "Jane"},
		},
	})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insertBase, ast.CompactStyle()))
	require.NoError(t, err)

	res, err := DeriveSwitch(base, SwitchSpec{
		Branches: []SwitchBranchSpec{
			{Label: "id_gt_1", TableName: "people_gt_1", Rules: []FilterRow{{Column: "id", Operation: FilterOperationGt, Value: 1}}},
			{Label: "name_john", TableName: "people_john", Rules: []FilterRow{{Column: "name", Operation: FilterOperationEq, Value: "John"}}},
			{Label: "match_all", TableName: "people_all", Rules: nil},
		},
		DefaultLabel:     "default",
		DefaultTableName: "people_default",
	})
	require.NoError(t, err)

	for _, b := range res.Branches {
		createDerived, err := BuildCreateTable(b.Table)
		require.NoError(t, err)
		_, err = db.Exec(formatter.FormatStatement(createDerived, ast.CompactStyle()))
		require.NoError(t, err)
	}

	createDefault, err := BuildCreateTable(res.DefaultBranch.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createDefault, ast.CompactStyle()))
	require.NoError(t, err)

	for _, backfill := range res.Backfills {
		_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
		require.NoError(t, err)
	}
	_, err = db.Exec(formatter.FormatStatement(res.DefaultBackfill, ast.CompactStyle()))
	require.NoError(t, err)

	var gtCount, johnCount, allCount, defaultCount int
	err = db.QueryRow("SELECT COUNT(*) FROM people_gt_1").Scan(&gtCount)
	require.NoError(t, err)
	err = db.QueryRow("SELECT COUNT(*) FROM people_john").Scan(&johnCount)
	require.NoError(t, err)
	err = db.QueryRow("SELECT COUNT(*) FROM people_all").Scan(&allCount)
	require.NoError(t, err)
	err = db.QueryRow("SELECT COUNT(*) FROM people_default").Scan(&defaultCount)
	require.NoError(t, err)

	assert.Equal(t, 3, gtCount)
	assert.Equal(t, 2, johnCount)
	assert.Equal(t, 4, allCount)
	assert.Equal(t, 0, defaultCount)
}
