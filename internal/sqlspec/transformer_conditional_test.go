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

func TestDeriveConditional_DefaultModeIsAll(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}
	res, err := DeriveConditional(base, "people_if", "people_else", ConditionalSpec{
		Rules: []FilterRow{{Column: "id", Operation: FilterOperationGt, Value: 1}, {Column: "name", Operation: FilterOperationEq, Value: "John"}},
	})
	require.NoError(t, err)

	ifQuery := res.IfBackfill.Query.(*ast.SelectStatement)
	where, ok := ifQuery.Where.(*ast.BinaryExpression)
	require.True(t, ok)
	assert.Equal(t, "AND", where.Operator)
}

func TestDeriveConditional_ModeAnyBuildsOrPredicate(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}
	res, err := DeriveConditional(base, "people_if", "people_else", ConditionalSpec{
		Mode:  ConditionalModeAny,
		Rules: []FilterRow{{Column: "id", Operation: FilterOperationGt, Value: 1}, {Column: "name", Operation: FilterOperationEq, Value: "John"}},
	})
	require.NoError(t, err)

	ifQuery := res.IfBackfill.Query.(*ast.SelectStatement)
	where, ok := ifQuery.Where.(*ast.BinaryExpression)
	require.True(t, ok)
	assert.Equal(t, "OR", where.Operator)

	elseQuery := res.ElseBackfill.Query.(*ast.SelectStatement)
	elseWhere, ok := elseQuery.Where.(*ast.BinaryExpression)
	require.True(t, ok)
	assert.Equal(t, "AND", elseWhere.Operator)
}

func TestDeriveConditional_EmptyRulesSendsAllToIf(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}
	res, err := DeriveConditional(base, "people_if", "people_else", ConditionalSpec{})
	require.NoError(t, err)

	ifQuery := res.IfBackfill.Query.(*ast.SelectStatement)
	assert.Nil(t, ifQuery.Where)

	elseQuery := res.ElseBackfill.Query.(*ast.SelectStatement)
	where, ok := elseQuery.Where.(*ast.BinaryExpression)
	require.True(t, ok)
	assert.Equal(t, "=", where.Operator)
}

func TestDeriveConditional_InvalidInputs(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}
	tests := []struct {
		name string
		base TableSpec
		ifT  string
		elT  string
		spec ConditionalSpec
	}{
		{name: "base required", base: TableSpec{}, ifT: "a", elT: "b", spec: ConditionalSpec{}},
		{name: "if required", base: base, ifT: "", elT: "b", spec: ConditionalSpec{}},
		{name: "else required", base: base, ifT: "a", elT: "", spec: ConditionalSpec{}},
		{name: "if else different", base: base, ifT: "a", elT: "A", spec: ConditionalSpec{}},
		{name: "invalid mode", base: base, ifT: "a", elT: "b", spec: ConditionalSpec{Mode: ConditionalMode("none")}},
		{name: "missing column", base: base, ifT: "a", elT: "b", spec: ConditionalSpec{Rules: []FilterRow{{Column: "unknown", Operation: FilterOperationEq, Value: 1}}}},
		{name: "unsupported operation", base: base, ifT: "a", elT: "b", spec: ConditionalSpec{Rules: []FilterRow{{Column: "id", Operation: FilterOperation("bad"), Value: 1}}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := DeriveConditional(tc.base, tc.ifT, tc.elT, tc.spec)
			assert.Error(t, err)
		})
	}
}

func TestDeriveConditional_DuckDBBranchingAllAndAny(t *testing.T) {
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
		},
	})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insertBase, ast.CompactStyle()))
	require.NoError(t, err)

	allRes, err := DeriveConditional(base, "people_if_all", "people_else_all", ConditionalSpec{
		Mode: ConditionalModeAll,
		Rules: []FilterRow{
			{Column: "id", Operation: FilterOperationGt, Value: 1},
			{Column: "name", Operation: FilterOperationEq, Value: "John"},
		},
	})
	require.NoError(t, err)

	createIfAll, err := BuildCreateTable(allRes.IfBranch.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createIfAll, ast.CompactStyle()))
	require.NoError(t, err)
	createElseAll, err := BuildCreateTable(allRes.ElseBranch.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createElseAll, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(allRes.IfBackfill, ast.CompactStyle()))
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(allRes.ElseBackfill, ast.CompactStyle()))
	require.NoError(t, err)

	anyRes, err := DeriveConditional(base, "people_if_any", "people_else_any", ConditionalSpec{
		Mode: ConditionalModeAny,
		Rules: []FilterRow{
			{Column: "id", Operation: FilterOperationEq, Value: 1},
			{Column: "name", Operation: FilterOperationEq, Value: "Doe"},
		},
	})
	require.NoError(t, err)

	createIfAny, err := BuildCreateTable(anyRes.IfBranch.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createIfAny, ast.CompactStyle()))
	require.NoError(t, err)
	createElseAny, err := BuildCreateTable(anyRes.ElseBranch.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createElseAny, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(anyRes.IfBackfill, ast.CompactStyle()))
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(anyRes.ElseBackfill, ast.CompactStyle()))
	require.NoError(t, err)

	var ifAllCount, elseAllCount int
	err = db.QueryRow("SELECT COUNT(*) FROM people_if_all").Scan(&ifAllCount)
	require.NoError(t, err)
	err = db.QueryRow("SELECT COUNT(*) FROM people_else_all").Scan(&elseAllCount)
	require.NoError(t, err)
	assert.Equal(t, 1, ifAllCount)
	assert.Equal(t, 2, elseAllCount)

	var ifAnyCount, elseAnyCount int
	err = db.QueryRow("SELECT COUNT(*) FROM people_if_any").Scan(&ifAnyCount)
	require.NoError(t, err)
	err = db.QueryRow("SELECT COUNT(*) FROM people_else_any").Scan(&elseAnyCount)
	require.NoError(t, err)
	assert.Equal(t, 2, ifAnyCount)
	assert.Equal(t, 1, elseAnyCount)

	var baseCount int
	err = db.QueryRow("SELECT COUNT(*) FROM people").Scan(&baseCount)
	require.NoError(t, err)
	assert.Equal(t, baseCount, ifAnyCount+elseAnyCount)
	assert.Equal(t, baseCount, ifAllCount+elseAllCount)
}
