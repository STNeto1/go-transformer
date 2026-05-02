package sqlspec

import (
	"fmt"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

func DeriveSelect(base TableSpec, derivedTableName string, selectedColumns []string) (ResolvedBranch, *ast.InsertStatement, error) {
	if base.Name == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("base table name is required")
	}
	if derivedTableName == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("derived table name is required")
	}
	if len(selectedColumns) == 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("you need to have some list of columns selected")
	}

	cols := make([]ColumnSpec, 0)

	var invalidSelections []string
	for _, selectedCol := range selectedColumns {
		matched := false
		for _, baseCol := range base.Columns {
			if strings.EqualFold(baseCol.Name, selectedCol) {
				cols = append(cols, baseCol)
				matched = true
				break
			}
		}
		if !matched {
			invalidSelections = append(invalidSelections, selectedCol)
		}
	}

	if len(invalidSelections) > 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("invalid columns selected: %s", strings.Join(invalidSelections, ", "))
	}

	resolved := ResolvedBranch{
		Table: TableSpec{
			Name:    derivedTableName,
			Columns: cols,
		},
	}

	stmt, err := BuildBackfillInsert(base.Name, resolved)
	if err != nil {
		return ResolvedBranch{}, nil, err
	}

	return resolved, stmt, nil
}
