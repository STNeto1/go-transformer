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

func TestResolveBranchAddColumnStaticValue(t *testing.T) {
	t.Parallel()

	base := TableSpec{
		Name: "people",
		Columns: []ColumnSpec{
			{Name: "id", SQLType: "integer"},
			{Name: "name", SQLType: "varchar"},
		},
	}

	branch, err := ResolveBranch(base, TableBranchSpec{
		TargetTable: "people_branch",
		Ops: []SchemaOp{{
			Type:        SchemaOpAddColumn,
			Column:      ColumnSpec{Name: "foo", SQLType: "varchar"},
			StaticValue: "bar",
		}},
	})
	require.NoError(t, err)
	assert.Equal(t, "people_branch", branch.Table.Name)
	assert.Len(t, branch.Table.Columns, 3)
	assert.Equal(t, "foo", branch.Table.Columns[2].Name)
	assert.Equal(t, "bar", branch.StaticValues["foo"])
}

func TestResolveBranchRejectsInvalidDrop(t *testing.T) {
	t.Parallel()

	base := TableSpec{
		Name: "people",
		Columns: []ColumnSpec{
			{Name: "id", SQLType: "integer"},
		},
	}

	_, err := ResolveBranch(base, TableBranchSpec{
		TargetTable: "people_branch",
		Ops: []SchemaOp{{
			Type:       SchemaOpDropColumn,
			ColumnName: "name",
		}},
	})
	assert.Error(t, err)
}

func TestBranchPhysicalTableCreationAndBackfill(t *testing.T) {
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

	branch, err := ResolveBranch(base, TableBranchSpec{
		TargetTable: "people_branch",
		Ops: []SchemaOp{{
			Type:        SchemaOpAddColumn,
			Column:      ColumnSpec{Name: "foo", SQLType: "varchar"},
			StaticValue: "bar",
		}},
	})
	require.NoError(t, err)

	createBranch, err := BuildCreateTable(branch.Table)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(createBranch, ast.CompactStyle()))
	require.NoError(t, err)

	backfill, err := BuildBackfillInsert(base.Name, branch)
	require.NoError(t, err)
	_, err = db.Exec(formatter.FormatStatement(backfill, ast.CompactStyle()))
	require.NoError(t, err)

	rows, err := db.Query("SELECT id, name, foo FROM people_branch ORDER BY id")
	require.NoError(t, err)
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id int
		var name, foo string
		err := rows.Scan(&id, &name, &foo)
		require.NoError(t, err)
		assert.Equal(t, "bar", foo)
		count++
	}

	require.NoError(t, rows.Err())
	assert.Equal(t, 2, count)
}
