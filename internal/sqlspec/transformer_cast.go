package sqlspec

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

type CastColumn struct {
	Column  string
	SQLType string
}

func DeriveCastColumns(base TableSpec, derivedTableName string, casts []CastColumn) (ResolvedBranch, *ast.InsertStatement, error) {
	if base.Name == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("base table name is required")
	}
	if derivedTableName == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("derived table name is required")
	}
	if len(casts) == 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("at least one cast is required")
	}

	resolvedCols := make([]ColumnSpec, len(base.Columns))
	copy(resolvedCols, base.Columns)

	targetTypeByColumn := map[string]string{}
	for _, cast := range casts {
		if cast.Column == "" || cast.SQLType == "" {
			return ResolvedBranch{}, nil, fmt.Errorf("cast requires column and SQL type")
		}

		idx := slices.IndexFunc(base.Columns, func(c ColumnSpec) bool {
			return strings.EqualFold(c.Name, cast.Column)
		})
		if idx == -1 {
			return ResolvedBranch{}, nil, fmt.Errorf("column %q does not exist", cast.Column)
		}

		canonical := base.Columns[idx].Name
		if _, ok := targetTypeByColumn[canonical]; ok {
			return ResolvedBranch{}, nil, fmt.Errorf("duplicate cast column %q", cast.Column)
		}

		if strings.EqualFold(base.Columns[idx].SQLType, cast.SQLType) {
			return ResolvedBranch{}, nil, fmt.Errorf("cast for column %q is a no-op", cast.Column)
		}

		targetTypeByColumn[canonical] = cast.SQLType
	}

	for i := range resolvedCols {
		if targetType, ok := targetTypeByColumn[resolvedCols[i].Name]; ok {
			resolvedCols[i].SQLType = targetType
		}
	}

	resolved := ResolvedBranch{
		Table: TableSpec{
			Name:    derivedTableName,
			Columns: resolvedCols,
		},
	}

	selectFn := func(selectAst *ast.SelectStatement) {
		columns := make([]ast.Expression, 0, len(base.Columns))
		for _, sourceCol := range base.Columns {
			targetType, casted := targetTypeByColumn[sourceCol.Name]
			if !casted {
				columns = append(columns, &ast.Identifier{Name: sourceCol.Name})
				continue
			}
			columns = append(columns, &ast.CastExpression{
				Expr: &ast.Identifier{Name: sourceCol.Name},
				Type: targetType,
				Try:  false,
			})
		}
		selectAst.Columns = columns
	}

	stmt, err := BuildBackfillInsert(base.Name, resolved, selectFn)
	if err != nil {
		return ResolvedBranch{}, nil, err
	}

	return resolved, stmt, nil
}
