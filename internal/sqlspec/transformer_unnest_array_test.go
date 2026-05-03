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

func TestDeriveUnnestArray_ValidSpecBuildsProjection(t *testing.T) {
	t.Parallel()

	base := TableSpec{
		Name: "events",
		Columns: []ColumnSpec{
			{Name: "id", SQLType: "integer"},
			{Name: "tags", SQLType: "integer[]"},
		},
	}

	resolved, stmt, err := DeriveUnnestArray(base, "events_unnested", UnnestArraySpec{
		ArrayColumn:  "tags",
		OutputColumn: ColumnSpec{Name: "tag", SQLType: "integer"},
	})
	require.NoError(t, err)
	require.NotNil(t, stmt)

	require.Len(t, resolved.Table.Columns, 3)
	assert.Equal(t, "tag", resolved.Table.Columns[2].Name)

	query := stmt.Query.(*ast.SelectStatement)
	require.Len(t, query.Columns, 3)

	fn, ok := query.Columns[2].(*ast.FunctionCall)
	require.True(t, ok)
	assert.Equal(t, "UNNEST", fn.Name)
	require.Len(t, fn.Arguments, 1)
}

func TestDeriveUnnestArray_InvalidInputs(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "events", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "tags", SQLType: "integer[]"}}}
	tests := []struct {
		name string
		base TableSpec
		tbl  string
		spec UnnestArraySpec
	}{
		{name: "base required", base: TableSpec{}, tbl: "x", spec: UnnestArraySpec{ArrayColumn: "tags", OutputColumn: ColumnSpec{Name: "tag", SQLType: "integer"}}},
		{name: "table required", base: base, tbl: "", spec: UnnestArraySpec{ArrayColumn: "tags", OutputColumn: ColumnSpec{Name: "tag", SQLType: "integer"}}},
		{name: "base cols required", base: TableSpec{Name: "events"}, tbl: "x", spec: UnnestArraySpec{ArrayColumn: "tags", OutputColumn: ColumnSpec{Name: "tag", SQLType: "integer"}}},
		{name: "array required", base: base, tbl: "x", spec: UnnestArraySpec{OutputColumn: ColumnSpec{Name: "tag", SQLType: "integer"}}},
		{name: "output name required", base: base, tbl: "x", spec: UnnestArraySpec{ArrayColumn: "tags", OutputColumn: ColumnSpec{SQLType: "integer"}}},
		{name: "output type required", base: base, tbl: "x", spec: UnnestArraySpec{ArrayColumn: "tags", OutputColumn: ColumnSpec{Name: "tag"}}},
		{name: "array missing", base: base, tbl: "x", spec: UnnestArraySpec{ArrayColumn: "missing", OutputColumn: ColumnSpec{Name: "tag", SQLType: "integer"}}},
		{name: "output collision", base: base, tbl: "x", spec: UnnestArraySpec{ArrayColumn: "tags", OutputColumn: ColumnSpec{Name: "id", SQLType: "integer"}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := DeriveUnnestArray(tc.base, tc.tbl, tc.spec)
			assert.Error(t, err)
		})
	}
}

func TestDeriveUnnestArray_DuckDBBackfill(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()

	base := TableSpec{
		Name: "events",
		Columns: []ColumnSpec{
			{Name: "id", SQLType: "integer"},
			{Name: "tags", SQLType: "integer[]"},
		},
	}

	createBase, err := BuildCreateTable(base)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createBase, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec("INSERT INTO events (id, tags) VALUES (1, [10, 20]), (2, []), (3, NULL), (4, [30])")
	require.NoError(t, err)

	resolved, backfill, err := DeriveUnnestArray(base, "events_unnested", UnnestArraySpec{
		ArrayColumn:  "tags",
		OutputColumn: ColumnSpec{Name: "tag", SQLType: "integer"},
	})
	require.NoError(t, err)

	createDerived, err := BuildCreateTable(resolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createDerived, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
	require.NoError(t, err)

	rows, err := db.Query("SELECT id, tag FROM events_unnested ORDER BY id, tag")
	require.NoError(t, err)
	defer rows.Close()

	var got []string
	for rows.Next() {
		var id int
		var tag int
		err := rows.Scan(&id, &tag)
		require.NoError(t, err)
		if id == 1 && tag == 10 {
			got = append(got, "1:10")
		} else if id == 1 && tag == 20 {
			got = append(got, "1:20")
		} else if id == 4 && tag == 30 {
			got = append(got, "4:30")
		}
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"1:10", "1:20", "4:30"}, got)
}
