package sqlspec

import (
	"fmt"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

type MergeUnionMode string

const (
	MergeUnionModeStrictPositional MergeUnionMode = "strict_positional"
	MergeUnionModeAlignByName      MergeUnionMode = "align_by_name"
)

type MergeUnionSpec struct {
	Mode MergeUnionMode
}

func DeriveMergeUnion(inputs []TableSpec, derivedTableName string) (ResolvedBranch, *ast.InsertStatement, error) {
	return DeriveMergeUnionWithSpec(inputs, derivedTableName, MergeUnionSpec{Mode: MergeUnionModeStrictPositional})
}

func DeriveMergeUnionWithSpec(inputs []TableSpec, derivedTableName string, spec MergeUnionSpec) (ResolvedBranch, *ast.InsertStatement, error) {
	if derivedTableName == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("derived table name is required")
	}
	if len(inputs) < 2 {
		return ResolvedBranch{}, nil, fmt.Errorf("at least two input tables are required")
	}

	mode := spec.Mode
	if mode == "" {
		mode = MergeUnionModeStrictPositional
	}
	if mode != MergeUnionModeStrictPositional && mode != MergeUnionModeAlignByName {
		return ResolvedBranch{}, nil, fmt.Errorf("unsupported merge union mode %q", mode)
	}

	base := inputs[0]
	if base.Name == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("input table name is required")
	}
	if len(base.Columns) == 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("input table requires columns")
	}

	for idx, c := range base.Columns {
		if c.Name == "" {
			return ResolvedBranch{}, nil, fmt.Errorf("input table %q column %d has empty name", base.Name, idx)
		}
		if c.SQLType == "" {
			return ResolvedBranch{}, nil, fmt.Errorf("input table %q column %q has empty SQL type", base.Name, c.Name)
		}
	}

	if err := validateUniqueColumns(base.Columns); err != nil {
		return ResolvedBranch{}, nil, err
	}

	resolvedCols := make([]ColumnSpec, len(base.Columns))
	copy(resolvedCols, base.Columns)

	selects := make([]*ast.SelectStatement, 0, len(inputs))
	for i, in := range inputs {
		if in.Name == "" {
			return ResolvedBranch{}, nil, fmt.Errorf("input table name is required")
		}
		if len(in.Columns) == 0 {
			return ResolvedBranch{}, nil, fmt.Errorf("input table %q requires columns", in.Name)
		}

		var cols []ast.Expression
		var err error
		if i == 0 {
			cols = make([]ast.Expression, 0, len(base.Columns))
			for _, baseCol := range base.Columns {
				cols = append(cols, &ast.Identifier{Name: baseCol.Name})
			}
		} else {
			switch mode {
			case MergeUnionModeStrictPositional:
				err = validateMergeUnionCompatibility(base, in)
				if err == nil {
					cols = make([]ast.Expression, 0, len(base.Columns))
					for _, baseCol := range base.Columns {
						cols = append(cols, &ast.Identifier{Name: baseCol.Name})
					}
				}
			case MergeUnionModeAlignByName:
				cols, err = buildMergeUnionAlignedColumns(base, in)
			}
			if err != nil {
				return ResolvedBranch{}, nil, err
			}
		}

		selects = append(selects, &ast.SelectStatement{Columns: cols, From: []ast.TableReference{{Name: in.Name}}})
	}

	query := ast.QueryExpression(selects[0])
	for i := 1; i < len(selects); i++ {
		query = &ast.SetOperation{Left: query.(ast.Statement), Operator: "UNION", Right: selects[i], All: true}
	}

	insertCols := make([]ast.Expression, 0, len(resolvedCols))
	for _, c := range resolvedCols {
		insertCols = append(insertCols, &ast.Identifier{Name: c.Name})
	}

	resolved := ResolvedBranch{Table: TableSpec{Name: derivedTableName, Columns: resolvedCols}}
	stmt := &ast.InsertStatement{TableName: resolved.Table.Name, Columns: insertCols, Query: query}
	return resolved, stmt, nil
}

func validateUniqueColumns(cols []ColumnSpec) error {
	seen := map[string]struct{}{}
	for _, c := range cols {
		k := strings.ToLower(c.Name)
		if _, ok := seen[k]; ok {
			return fmt.Errorf("duplicate output column %q", c.Name)
		}
		seen[k] = struct{}{}
	}
	return nil
}

func validateMergeUnionCompatibility(base TableSpec, in TableSpec) error {
	if len(in.Columns) != len(base.Columns) {
		return fmt.Errorf("input table %q column count mismatch: expected %d, got %d", in.Name, len(base.Columns), len(in.Columns))
	}

	for idx := range base.Columns {
		bc := base.Columns[idx]
		ic := in.Columns[idx]
		if !strings.EqualFold(bc.Name, ic.Name) {
			return fmt.Errorf("input table %q column %d name mismatch: expected %q, got %q", in.Name, idx, bc.Name, ic.Name)
		}
		if !strings.EqualFold(bc.SQLType, ic.SQLType) {
			return fmt.Errorf("input table %q column %q type mismatch: expected %q, got %q", in.Name, bc.Name, bc.SQLType, ic.SQLType)
		}
	}

	return nil
}

func buildMergeUnionAlignedColumns(base TableSpec, in TableSpec) ([]ast.Expression, error) {
	inByName := map[string]ColumnSpec{}
	for _, c := range in.Columns {
		if c.Name == "" {
			return nil, fmt.Errorf("input table %q has empty column name", in.Name)
		}
		k := strings.ToLower(c.Name)
		if _, exists := inByName[k]; exists {
			return nil, fmt.Errorf("input table %q has duplicate column %q", in.Name, c.Name)
		}
		inByName[k] = c
	}

	if len(inByName) != len(base.Columns) {
		return nil, fmt.Errorf("input table %q has extra or missing columns for align_by_name mode", in.Name)
	}

	cols := make([]ast.Expression, 0, len(base.Columns))
	for _, bc := range base.Columns {
		ic, ok := inByName[strings.ToLower(bc.Name)]
		if !ok {
			return nil, fmt.Errorf("input table %q is missing required column %q", in.Name, bc.Name)
		}
		if !strings.EqualFold(bc.SQLType, ic.SQLType) {
			return nil, fmt.Errorf("input table %q column %q type mismatch: expected %q, got %q", in.Name, bc.Name, bc.SQLType, ic.SQLType)
		}
		cols = append(cols, &ast.Identifier{Name: ic.Name})
	}

	return cols, nil
}
