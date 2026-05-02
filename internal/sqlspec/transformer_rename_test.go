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

func TestDeriveRenameColumns_ValidSingleRename(t *testing.T) {
	t.Parallel()

	base := TableSpec{
		Name: "people",
		Columns: []ColumnSpec{
			{Name: "id", SQLType: "integer"},
			{Name: "name", SQLType: "varchar"},
		},
	}

	resolved, stmt, err := DeriveRenameColumns(base, "people_renamed", []RenameColumn{{From: "name", To: "full_name"}})
	require.NoError(t, err)
	require.NotNil(t, stmt)

	assert.Equal(t, "people_renamed", resolved.Table.Name)
	require.Len(t, resolved.Table.Columns, 2)
	assert.Equal(t, "id", resolved.Table.Columns[0].Name)
	assert.Equal(t, "full_name", resolved.Table.Columns[1].Name)

	query, ok := stmt.Query.(*ast.SelectStatement)
	require.True(t, ok)
	require.Len(t, query.Columns, 2)

	q0, ok := query.Columns[0].(*ast.Identifier)
	require.True(t, ok)
	assert.Equal(t, "id", q0.Name)

	q1, ok := query.Columns[1].(*ast.Identifier)
	require.True(t, ok)
	assert.Equal(t, "name", q1.Name)
}

func TestDeriveRenameColumns_ValidMultipleRenames(t *testing.T) {
	t.Parallel()

	base := TableSpec{
		Name: "people",
		Columns: []ColumnSpec{
			{Name: "id", SQLType: "integer"},
			{Name: "name", SQLType: "varchar"},
			{Name: "age", SQLType: "integer"},
		},
	}

	resolved, _, err := DeriveRenameColumns(base, "people_renamed", []RenameColumn{{From: "name", To: "full_name"}, {From: "age", To: "years_old"}})
	require.NoError(t, err)
	require.Len(t, resolved.Table.Columns, 3)
	assert.Equal(t, "id", resolved.Table.Columns[0].Name)
	assert.Equal(t, "full_name", resolved.Table.Columns[1].Name)
	assert.Equal(t, "years_old", resolved.Table.Columns[2].Name)
}

func TestDeriveRenameColumns_CaseInsensitiveFromMatch(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "Name", SQLType: "varchar"}}}
	resolved, stmt, err := DeriveRenameColumns(base, "people_renamed", []RenameColumn{{From: "name", To: "full_name"}})
	require.NoError(t, err)
	require.Len(t, resolved.Table.Columns, 1)
	assert.Equal(t, "full_name", resolved.Table.Columns[0].Name)

	query := stmt.Query.(*ast.SelectStatement)
	source := query.Columns[0].(*ast.Identifier)
	assert.Equal(t, "Name", source.Name)
}

func TestDeriveRenameColumns_InvalidInputs(t *testing.T) {
	t.Parallel()

	base := TableSpec{Name: "people", Columns: []ColumnSpec{{Name: "id", SQLType: "integer"}, {Name: "name", SQLType: "varchar"}}}

	tests := []struct {
		name    string
		base    TableSpec
		table   string
		renames []RenameColumn
	}{
		{name: "base required", base: TableSpec{}, table: "x", renames: []RenameColumn{{From: "id", To: "person_id"}}},
		{name: "table required", base: base, table: "", renames: []RenameColumn{{From: "id", To: "person_id"}}},
		{name: "renames required", base: base, table: "x", renames: nil},
		{name: "from required", base: base, table: "x", renames: []RenameColumn{{From: "", To: "person_id"}}},
		{name: "to required", base: base, table: "x", renames: []RenameColumn{{From: "id", To: ""}}},
		{name: "from must exist", base: base, table: "x", renames: []RenameColumn{{From: "unknown", To: "x"}}},
		{name: "duplicate source", base: base, table: "x", renames: []RenameColumn{{From: "id", To: "person_id"}, {From: "ID", To: "person_id2"}}},
		{name: "duplicate target", base: base, table: "x", renames: []RenameColumn{{From: "id", To: "x"}, {From: "name", To: "X"}}},
		{name: "target collides with untouched", base: base, table: "x", renames: []RenameColumn{{From: "id", To: "name"}}},
		{name: "no-op rename strict", base: base, table: "x", renames: []RenameColumn{{From: "name", To: "Name"}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := DeriveRenameColumns(tc.base, tc.table, tc.renames)
			assert.Error(t, err)
		})
	}
}

func TestDeriveRenameColumns_DuckDBBackfillRenamesColumns(t *testing.T) {
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
		},
	})
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(insertBase, ast.CompactStyle()))
	require.NoError(t, err)

	resolved, backfill, err := DeriveRenameColumns(base, "people_renamed", []RenameColumn{{From: "name", To: "full_name"}})
	require.NoError(t, err)

	createDerived, err := BuildCreateTable(resolved.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createDerived, ast.CompactStyle()))
	require.NoError(t, err)

	_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
	require.NoError(t, err)

	rows, err := db.Query("SELECT id, full_name FROM people_renamed ORDER BY id")
	require.NoError(t, err)
	defer rows.Close()

	var names []string
	for rows.Next() {
		var id int
		var fullName string
		err := rows.Scan(&id, &fullName)
		require.NoError(t, err)
		names = append(names, fullName)
	}

	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"John", "Doe"}, names)
}
