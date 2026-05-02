package sqlspec

import (
	"fmt"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

type LimitSpec struct {
	Count  int
	Offset *int
}

func DeriveLimit(base TableSpec, derivedTableName string, spec LimitSpec) (ResolvedBranch, *ast.InsertStatement, error) {
	if base.Name == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("base table name is required")
	}
	if derivedTableName == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("derived table name is required")
	}
	if spec.Count <= 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("limit must be greater than zero")
	}
	if spec.Offset != nil && *spec.Offset < 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("offset must be greater than or equal to zero")
	}

	cols := make([]ColumnSpec, len(base.Columns))
	copy(cols, base.Columns)

	resolved := ResolvedBranch{
		Table: TableSpec{
			Name:    derivedTableName,
			Columns: cols,
		},
	}

	limit := spec.Count
	selectFn := func(selectAst *ast.SelectStatement) {
		selectAst.Limit = &limit
		if spec.Offset != nil {
			offset := *spec.Offset
			selectAst.Offset = &offset
		}
	}

	stmt, err := BuildBackfillInsert(base.Name, resolved, selectFn)
	if err != nil {
		return ResolvedBranch{}, nil, err
	}

	return resolved, stmt, nil
}
