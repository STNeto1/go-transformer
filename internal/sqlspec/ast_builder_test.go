package sqlspec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCreateTable(t *testing.T) {
	t.Parallel()

	stmt, err := BuildCreateTable(TableSpec{
		Name: "people",
		Columns: []ColumnSpec{
			{Name: "id", SQLType: "integer"},
			{Name: "name", SQLType: "varchar"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "people", stmt.Name)
	assert.Len(t, stmt.Columns, 2)
}

func TestBuildCreateTableValidation(t *testing.T) {
	t.Parallel()

	_, err := BuildCreateTable(TableSpec{})
	assert.Error(t, err)

	_, err = BuildCreateTable(TableSpec{Name: "x"})
	assert.Error(t, err)
}

func TestBuildInsert(t *testing.T) {
	t.Parallel()

	stmt, err := BuildInsert(InsertSpec{
		Table:   "people",
		Columns: []string{"id", "name"},
		Rows: []map[string]any{
			{"id": 1, "name": "John"},
			{"id": 2, "name": "Doe"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "people", stmt.TableName)
	assert.Len(t, stmt.Columns, 2)
	assert.Len(t, stmt.Values, 2)
	assert.Len(t, stmt.Values[0], 2)
}

func TestBuildInsertValidation(t *testing.T) {
	t.Parallel()

	_, err := BuildInsert(InsertSpec{})
	assert.Error(t, err)

	_, err = BuildInsert(InsertSpec{Table: "people", Rows: []map[string]any{{"id": 1}}, Columns: []string{"id", "name"}})
	assert.Error(t, err)
}

func TestBuildSelect(t *testing.T) {
	t.Parallel()

	stmt, err := BuildSelect(SelectSpec{Table: "people", Columns: []string{"id", "name"}})
	require.NoError(t, err)
	assert.Len(t, stmt.Columns, 2)
	require.Len(t, stmt.From, 1)
	assert.Equal(t, "people", stmt.From[0].Name)
}

func TestToExpression(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		in    any
		isNil bool
	}{
		{name: "nil", in: nil, isNil: false},
		{name: "string", in: "x", isNil: false},
		{name: "bool", in: true, isNil: false},
		{name: "int", in: 1, isNil: false},
		{name: "float64", in: 1.2, isNil: false},
		{name: "unsupported", in: struct{}{}, isNil: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			expr := ToExpression(tc.in)
			if tc.isNil {
				assert.Nil(t, expr)
				return
			}
			assert.NotNil(t, expr)
		})
	}
}
