package sqlspec

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

type UnnestArraySpec struct {
	ArrayColumn  string
	OutputColumn ColumnSpec
}

func DeriveUnnestArray(base TableSpec, derivedTableName string, spec UnnestArraySpec) (ResolvedBranch, *ast.InsertStatement, error) {
	if base.Name == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("base table name is required")
	}
	if derivedTableName == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("derived table name is required")
	}
	if len(base.Columns) == 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("base table requires columns")
	}
	if spec.ArrayColumn == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("array column is required")
	}
	if spec.OutputColumn.Name == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("output column name is required")
	}
	if spec.OutputColumn.SQLType == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("output column SQL type is required")
	}

	arrayIdx := slices.IndexFunc(base.Columns, func(c ColumnSpec) bool {
		return strings.EqualFold(c.Name, spec.ArrayColumn)
	})
	if arrayIdx == -1 {
		return ResolvedBranch{}, nil, fmt.Errorf("column %q does not exist", spec.ArrayColumn)
	}

	if slices.IndexFunc(base.Columns, func(c ColumnSpec) bool {
		return strings.EqualFold(c.Name, spec.OutputColumn.Name)
	}) >= 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("column %q already exists", spec.OutputColumn.Name)
	}

	cols := make([]ColumnSpec, len(base.Columns), len(base.Columns)+1)
	copy(cols, base.Columns)
	cols = append(cols, spec.OutputColumn)

	resolved := ResolvedBranch{Table: TableSpec{Name: derivedTableName, Columns: cols}}

	arrayColumnName := base.Columns[arrayIdx].Name
	selectFn := func(s *ast.SelectStatement) {
		selectCols := make([]ast.Expression, 0, len(base.Columns)+1)
		for _, c := range base.Columns {
			selectCols = append(selectCols, &ast.Identifier{Name: c.Name})
		}
		selectCols = append(selectCols, &ast.FunctionCall{
			Name:      "UNNEST",
			Arguments: []ast.Expression{&ast.Identifier{Name: arrayColumnName}},
		})
		s.Columns = selectCols
	}

	stmt, err := BuildBackfillInsert(base.Name, resolved, selectFn)
	if err != nil {
		return ResolvedBranch{}, nil, err
	}

	return resolved, stmt, nil
}
