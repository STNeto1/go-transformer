package sqlspec

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

func DeriveConstantColumn(base TableSpec, derivedTableName string, column ColumnSpec, value any) (ResolvedBranch, *ast.InsertStatement, error) {
	if base.Name == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("base table name is required")
	}
	if derivedTableName == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("derived table name is required")
	}
	if column.Name == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("constant column name is required")
	}
	if column.SQLType == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("constant column SQL type is required")
	}
	if ToExpression(value) == nil {
		return ResolvedBranch{}, nil, fmt.Errorf("unsupported constant value type %T", value)
	}

	if slices.IndexFunc(base.Columns, func(c ColumnSpec) bool {
		return strings.EqualFold(c.Name, column.Name)
	}) >= 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("column %q already exists", column.Name)
	}

	cols := make([]ColumnSpec, len(base.Columns), len(base.Columns)+1)
	copy(cols, base.Columns)
	cols = append(cols, column)

	resolved := ResolvedBranch{
		Table: TableSpec{
			Name:    derivedTableName,
			Columns: cols,
		},
		StaticValues: map[string]any{column.Name: value},
	}

	stmt, err := BuildBackfillInsert(base.Name, resolved)
	if err != nil {
		return ResolvedBranch{}, nil, err
	}

	return resolved, stmt, nil
}
