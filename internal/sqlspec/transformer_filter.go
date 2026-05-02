package sqlspec

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

type FilterOperation string

const (
	FilterOperationEq         FilterOperation = "eq"
	FilterOperationNe         FilterOperation = "ne"
	FilterOperationContains   FilterOperation = "contains"
	FilterOperationStartsWith FilterOperation = "startsWith"
	FilterOperationEndsWith   FilterOperation = "endsWith"
	FilterOperationGt         FilterOperation = "gt"
	FilterOperationLt         FilterOperation = "lt"
)

var filterName = map[FilterOperation]string{
	FilterOperationEq:         "eq",
	FilterOperationNe:         "ne",
	FilterOperationContains:   "contains",
	FilterOperationStartsWith: "startsWith",
	FilterOperationEndsWith:   "endsWith",
	FilterOperationGt:         "gt",
	FilterOperationLt:         "lt",
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
	if _, ok := filterName[filter.Operation]; !ok {
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
	whereExpr, err := buildFilterWhereExpr(selectedColumn, filter)
	if err != nil {
		return ResolvedBranch{}, nil, err
	}

	resolved := ResolvedBranch{
		Table: TableSpec{
			Name:    derivedTableName,
			Columns: cols,
		},
	}

	selectFn := func(selectAst *ast.SelectStatement) {
		selectAst.Where = whereExpr
	}

	stmt, err := BuildBackfillInsert(base.Name, resolved, selectFn)
	if err != nil {
		return ResolvedBranch{}, nil, err
	}

	return resolved, stmt, nil
}

func buildFilterWhereExpr(selectedColumn ColumnSpec, filter FilterRow) (ast.Expression, error) {
	left := &ast.Identifier{Name: selectedColumn.Name}

	switch filter.Operation {
	case FilterOperationEq, FilterOperationNe, FilterOperationGt, FilterOperationLt:
		rightExpr := ToExpression(filter.Value)
		if rightExpr == nil {
			return nil, fmt.Errorf("unsupported filter value type %T", filter.Value)
		}

		op := "="
		switch filter.Operation {
		case FilterOperationNe:
			op = "!="
		case FilterOperationGt:
			op = ">"
		case FilterOperationLt:
			op = "<"
		}

		return &ast.BinaryExpression{Left: left, Operator: op, Right: rightExpr}, nil

	case FilterOperationContains, FilterOperationStartsWith, FilterOperationEndsWith:
		v, ok := filter.Value.(string)
		if !ok {
			return nil, fmt.Errorf("operation %q requires string value", filter.Operation)
		}

		pattern := v
		switch filter.Operation {
		case FilterOperationContains:
			pattern = "%" + v + "%"
		case FilterOperationStartsWith:
			pattern = v + "%"
		case FilterOperationEndsWith:
			pattern = "%" + v
		}

		return &ast.BinaryExpression{
			Left:     left,
			Operator: "LIKE",
			Right:    &ast.LiteralValue{Value: pattern, Type: "STRING"},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported filter operation %q", filter.Operation)
	}
}
