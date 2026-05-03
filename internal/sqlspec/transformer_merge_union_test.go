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

func TestDeriveMergeUnion_ValidTwoInputs(t *testing.T) {
	t.Parallel()

	inputs := []TableSpec{
		{Name: "people_a", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}},
		{Name: "people_b", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}},
	}

	resolved, stmt, err := DeriveMergeUnion(inputs, "people_merged")
	require.NoError(t, err)
	require.NotNil(t, stmt)
	assert.Equal(t, "people_merged", resolved.Table.Name)
	require.Len(t, resolved.Table.Columns, 2)

	setOp, ok := stmt.Query.(*ast.SetOperation)
	require.True(t, ok)
	assert.Equal(t, "UNION", setOp.Operator)
	assert.True(t, setOp.All)
}

func TestDeriveMergeUnion_ValidThreeInputs_LeftAssociative(t *testing.T) {
	t.Parallel()

	inputs := []TableSpec{
		{Name: "t1", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}},
		{Name: "t2", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}},
		{Name: "t3", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}},
	}

	_, stmt, err := DeriveMergeUnion(inputs, "merged")
	require.NoError(t, err)

	out, ok := stmt.Query.(*ast.SetOperation)
	require.True(t, ok)
	_, ok = out.Left.(*ast.SetOperation)
	require.True(t, ok)
	_, ok = out.Right.(*ast.SelectStatement)
	require.True(t, ok)
}

func TestDeriveMergeUnion_InvalidInputs(t *testing.T) {
	t.Parallel()

	good := []TableSpec{
		{Name: "a", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}},
		{Name: "b", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}},
	}

	tests := []struct {
		name   string
		inputs []TableSpec
		table  string
	}{
		{name: "derived required", inputs: good, table: ""},
		{name: "at least two inputs", inputs: good[:1], table: "x"},
		{name: "input name required", inputs: []TableSpec{{Name: "", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}, {Name: "b", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}}, table: "x"},
		{name: "input columns required", inputs: []TableSpec{{Name: "a", Columns: nil}, {Name: "b", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}}, table: "x"},
		{name: "column count mismatch", inputs: []TableSpec{{Name: "a", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}, {Name: "b", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}}, table: "x"},
		{name: "column name mismatch", inputs: []TableSpec{{Name: "a", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}, {Name: "b", Columns: []ColumnSpec{{Name: "user_id", SQLType: "integer"}}}}, table: "x"},
		{name: "column type mismatch", inputs: []TableSpec{{Name: "a", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}, {Name: "b", Columns: []ColumnSpec{{Name: "id", SQLType: "varchar"}}}}, table: "x"},
		{name: "duplicate output column", inputs: []TableSpec{{Name: "a", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "ID", SQLType: "integer"}}}, {Name: "b", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "ID", SQLType: "integer"}}}}, table: "x"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := DeriveMergeUnion(tc.inputs, tc.table)
			assert.Error(t, err)
		})
	}
}

func TestDeriveMergeUnion_DuckDBBackfill_UnionAllPreservesDuplicates(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	a := TableSpec{Name: "people_a", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}
	b := TableSpec{Name: "people_b", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}

	createA, err := BuildCreateTable(a)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createA, ast.CompactStyle()))
	require.NoError(t, err)

	createB, err := BuildCreateTable(b)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createB, ast.CompactStyle()))
	require.NoError(t, err)

	insA, err := BuildInsert(InsertSpec{Table: "people_a", Columns: []string{"id", "name"}, Rows: []map[string]any{{"id": 1, "name": "John"}, {"id": 2, "name": "Doe"}}})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insA, ast.CompactStyle()))
	require.NoError(t, err)

	insB, err := BuildInsert(InsertSpec{Table: "people_b", Columns: []string{"id", "name"}, Rows: []map[string]any{{"id": 2, "name": "Doe"}, {"id": 3, "name": "Mary"}}})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insB, ast.CompactStyle()))
	require.NoError(t, err)

	resolved, backfill, err := DeriveMergeUnion([]TableSpec{a, b}, "people_merged")
	require.NoError(t, err)

	createMerged, err := BuildCreateTable(resolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createMerged, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
	require.NoError(t, err)

	rows, err := db.Query("SELECT id, name FROM people_merged ORDER BY id, name")
	require.NoError(t, err)
	defer rows.Close()

	var got []string
	for rows.Next() {
		var id int
		var name string
		err := rows.Scan(&id, &name)
		require.NoError(t, err)
		got = append(got, name)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"John", "Doe", "Doe", "Mary"}, got)
}

func TestDeriveMergeUnionWithSpec_AlignByName_ReordersColumns(t *testing.T) {
	t.Parallel()

	inputs := []TableSpec{
		{Name: "a", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}},
		{Name: "b", Columns: []ColumnSpec{{Name: "name", SQLType: "varchar"}, {Name: "id", SQLType: "integer"}}},
	}

	resolved, stmt, err := DeriveMergeUnionWithSpec(inputs, "merged", MergeUnionSpec{Mode: MergeUnionModeAlignByName})
	require.NoError(t, err)
	require.Len(t, resolved.Table.Columns, 2)

	setOp, ok := stmt.Query.(*ast.SetOperation)
	require.True(t, ok)
	right, ok := setOp.Right.(*ast.SelectStatement)
	require.True(t, ok)
	require.Len(t, right.Columns, 2)
	idCol, ok := right.Columns[0].(*ast.Identifier)
	require.True(t, ok)
	assert.Equal(t, "id", idCol.Name)
	nameCol, ok := right.Columns[1].(*ast.Identifier)
	require.True(t, ok)
	assert.Equal(t, "name", nameCol.Name)
}

func TestDeriveMergeUnionWithSpec_InvalidAlignByNameInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		inputs []TableSpec
		spec   MergeUnionSpec
	}{
		{name: "invalid mode", inputs: []TableSpec{{Name: "a", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}, {Name: "b", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}}, spec: MergeUnionSpec{Mode: MergeUnionMode("x")}},
		{name: "missing column", inputs: []TableSpec{{Name: "a", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}, {Name: "b", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}}, spec: MergeUnionSpec{Mode: MergeUnionModeAlignByName}},
		{name: "extra column", inputs: []TableSpec{{Name: "a", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}, {Name: "b", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}}, spec: MergeUnionSpec{Mode: MergeUnionModeAlignByName}},
		{name: "type mismatch", inputs: []TableSpec{{Name: "a", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}}}, {Name: "b", Columns: []ColumnSpec{{Name: "id", SQLType: "varchar"}}}}, spec: MergeUnionSpec{Mode: MergeUnionModeAlignByName}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := DeriveMergeUnionWithSpec(tc.inputs, "merged", tc.spec)
			assert.Error(t, err)
		})
	}
}

func TestDeriveMergeUnionWithSpec_DuckDBAlignByNameBackfill(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	a := TableSpec{Name: "a", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}
	b := TableSpec{Name: "b", Columns: []ColumnSpec{{Name: "name", SQLType: "varchar"}, {Name: "id", SQLType: "integer"}}}

	createA, err := BuildCreateTable(a)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createA, ast.CompactStyle()))
	require.NoError(t, err)

	createB, err := BuildCreateTable(b)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createB, ast.CompactStyle()))
	require.NoError(t, err)

	insA, err := BuildInsert(InsertSpec{Table: "a", Columns: []string{"id", "name"}, Rows: []map[string]any{{"id": 1, "name": "Ann"}}})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insA, ast.CompactStyle()))
	require.NoError(t, err)

	insB, err := BuildInsert(InsertSpec{Table: "b", Columns: []string{"name", "id"}, Rows: []map[string]any{{"name": "Bob", "id": 2}}})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insB, ast.CompactStyle()))
	require.NoError(t, err)

	resolved, backfill, err := DeriveMergeUnionWithSpec([]TableSpec{a, b}, "merged", MergeUnionSpec{Mode: MergeUnionModeAlignByName})
	require.NoError(t, err)

	createMerged, err := BuildCreateTable(resolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createMerged, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
	require.NoError(t, err)

	rows, err := db.Query("SELECT id, name FROM merged ORDER BY id")
	require.NoError(t, err)
	defer rows.Close()

	var names []string
	for rows.Next() {
		var id int
		var name string
		err := rows.Scan(&id, &name)
		require.NoError(t, err)
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"Ann", "Bob"}, names)
}
