package sqlspec

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

type FilterOperation string

const (
	FilterOperationEq FilterOperation = "eq"
)

var filterName = map[FilterOperation]string{
	FilterOperationEq: "eq",
}

func (ss FilterOperation) String() string {
	return filterName[ss]
}

type FilterRow struct {
	Column    string
	Operation FilterOperation
	Value     any
}

func DeriveFilter(base TableSpec, derivedTableName string, filter FilterRow) (ResolvedBranch, *ast.InsertStatement, error) {
	if base.Name == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("base table name is required")
	}
	if derivedTableName == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("derived table name is required")
	}
	if filter.Column == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("filter column is required")
	}
	if filter.Operation != FilterOperationEq {
		return ResolvedBranch{}, nil, fmt.Errorf("unsupported filter operation %q", filter.Operation)
	}

	colIndex := slices.IndexFunc(base.Columns, func(col ColumnSpec) bool {
		return strings.EqualFold(col.Name, filter.Column)
	})

	if colIndex == -1 {
		return ResolvedBranch{}, nil, fmt.Errorf("column does not exist")
	}

	cols := make([]ColumnSpec, len(base.Columns))
	copy(cols, base.Columns)

	selectedColumn := cols[colIndex]
	rightExpr := ToExpression(filter.Value)
	if rightExpr == nil {
		return ResolvedBranch{}, nil, fmt.Errorf("unsupported filter value type %T", filter.Value)
	}

	resolved := ResolvedBranch{
		Table: TableSpec{
			Name:    derivedTableName,
			Columns: cols,
		},
	}

	selectFn := func(selectAst *ast.SelectStatement) {
		selectAst.Where = &ast.BinaryExpression{
			Left:     &ast.Identifier{Name: selectedColumn.Name},
			Operator: "=",
			Right:    rightExpr,
		}
	}

	stmt, err := BuildBackfillInsert(base.Name, resolved, selectFn)
	if err != nil {
		return ResolvedBranch{}, nil, err
	}

	return resolved, stmt, nil
}
