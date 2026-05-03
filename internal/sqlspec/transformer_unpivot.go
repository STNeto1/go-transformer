package sqlspec

import (
	"fmt"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

type UnpivotSpec struct {
	Passthrough []string
	NameColumn  ColumnSpec
	ValueColumn ColumnSpec
	InColumns   []string
}

func DeriveUnpivot(base TableSpec, derivedTableName string, spec UnpivotSpec) (ResolvedBranch, *ast.InsertStatement, error) {
	if base.Name == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("base table name is required")
	}
	if derivedTableName == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("derived table name is required")
	}
	if len(base.Columns) == 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("base table requires columns")
	}
	if len(spec.InColumns) == 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("at least one unpivot source column is required")
	}
	if spec.NameColumn.Name == "" || spec.NameColumn.SQLType == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("name column requires name and SQL type")
	}
	if spec.ValueColumn.Name == "" || spec.ValueColumn.SQLType == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("value column requires name and SQL type")
	}
	if strings.EqualFold(spec.NameColumn.Name, spec.ValueColumn.Name) {
		return ResolvedBranch{}, nil, fmt.Errorf("name and value columns must be different")
	}

	baseByName := map[string]ColumnSpec{}
	for _, c := range base.Columns {
		baseByName[strings.ToLower(c.Name)] = c
	}

	passthroughCols := make([]ColumnSpec, 0, len(spec.Passthrough))
	passthroughSeen := map[string]struct{}{}
	for _, c := range spec.Passthrough {
		if c == "" {
			return ResolvedBranch{}, nil, fmt.Errorf("passthrough column is required")
		}
		bc, ok := baseByName[strings.ToLower(c)]
		if !ok {
			return ResolvedBranch{}, nil, fmt.Errorf("passthrough column %q does not exist", c)
		}
		k := strings.ToLower(bc.Name)
		if _, exists := passthroughSeen[k]; exists {
			return ResolvedBranch{}, nil, fmt.Errorf("duplicate passthrough column %q", c)
		}
		passthroughSeen[k] = struct{}{}
		passthroughCols = append(passthroughCols, bc)
	}

	inColsCanonical := make([]string, 0, len(spec.InColumns))
	inSeen := map[string]struct{}{}
	for _, c := range spec.InColumns {
		if c == "" {
			return ResolvedBranch{}, nil, fmt.Errorf("unpivot source column is required")
		}
		bc, ok := baseByName[strings.ToLower(c)]
		if !ok {
			return ResolvedBranch{}, nil, fmt.Errorf("unpivot source column %q does not exist", c)
		}
		k := strings.ToLower(bc.Name)
		if _, exists := inSeen[k]; exists {
			return ResolvedBranch{}, nil, fmt.Errorf("duplicate unpivot source column %q", c)
		}
		if _, overlap := passthroughSeen[k]; overlap {
			return ResolvedBranch{}, nil, fmt.Errorf("column %q cannot be both passthrough and unpivot source", c)
		}
		inSeen[k] = struct{}{}
		inColsCanonical = append(inColsCanonical, bc.Name)
	}

	for _, c := range passthroughCols {
		if strings.EqualFold(c.Name, spec.NameColumn.Name) || strings.EqualFold(c.Name, spec.ValueColumn.Name) {
			return ResolvedBranch{}, nil, fmt.Errorf("output column collides with passthrough column %q", c.Name)
		}
	}

	resolvedCols := make([]ColumnSpec, 0, len(passthroughCols)+2)
	resolvedCols = append(resolvedCols, passthroughCols...)
	resolvedCols = append(resolvedCols, spec.NameColumn, spec.ValueColumn)

	insertCols := make([]ast.Expression, 0, len(resolvedCols))
	selectCols := make([]ast.Expression, 0, len(resolvedCols))
	for _, c := range passthroughCols {
		insertCols = append(insertCols, &ast.Identifier{Name: c.Name})
		selectCols = append(selectCols, &ast.Identifier{Name: c.Name})
	}
	insertCols = append(insertCols, &ast.Identifier{Name: spec.NameColumn.Name}, &ast.Identifier{Name: spec.ValueColumn.Name})
	selectCols = append(selectCols, &ast.Identifier{Name: spec.NameColumn.Name}, &ast.Identifier{Name: spec.ValueColumn.Name})

	query := &ast.SelectStatement{
		Columns: selectCols,
		From: []ast.TableReference{{
			Name: base.Name,
			Unpivot: &ast.UnpivotClause{
				ValueColumn: spec.ValueColumn.Name,
				NameColumn:  spec.NameColumn.Name,
				InColumns:   inColsCanonical,
			},
		}},
	}

	resolved := ResolvedBranch{Table: TableSpec{Name: derivedTableName, Columns: resolvedCols}}
	stmt := &ast.InsertStatement{TableName: derivedTableName, Columns: insertCols, Query: query}
	return resolved, stmt, nil
}
