package sqlspec

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

type FillReplaceRule struct {
	Column       string
	FillNullWith any
	ReplaceFrom  any
	ReplaceTo    any
}

func DeriveFillReplace(base TableSpec, derivedTableName string, rules []FillReplaceRule) (ResolvedBranch, *ast.InsertStatement, error) {
	if base.Name == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("base table name is required")
	}
	if derivedTableName == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("derived table name is required")
	}
	if len(rules) == 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("at least one fill/replace rule is required")
	}

	cols := make([]ColumnSpec, len(base.Columns))
	copy(cols, base.Columns)

	rulesByColumn := map[string]FillReplaceRule{}
	for _, rule := range rules {
		if rule.Column == "" {
			return ResolvedBranch{}, nil, fmt.Errorf("fill/replace column is required")
		}

		idx := slices.IndexFunc(base.Columns, func(c ColumnSpec) bool {
			return strings.EqualFold(c.Name, rule.Column)
		})
		if idx == -1 {
			return ResolvedBranch{}, nil, fmt.Errorf("column %q does not exist", rule.Column)
		}

		canonicalCol := base.Columns[idx].Name
		if _, exists := rulesByColumn[canonicalCol]; exists {
			return ResolvedBranch{}, nil, fmt.Errorf("duplicate fill/replace rule for column %q", rule.Column)
		}

		hasFill := rule.FillNullWith != nil
		hasReplaceFrom := rule.ReplaceFrom != nil
		hasReplaceTo := rule.ReplaceTo != nil

		if !hasFill && !hasReplaceFrom && !hasReplaceTo {
			return ResolvedBranch{}, nil, fmt.Errorf("fill/replace rule for column %q must define fill or replace", rule.Column)
		}
		if hasReplaceFrom != hasReplaceTo {
			return ResolvedBranch{}, nil, fmt.Errorf("replace rule for column %q requires both replace from and replace to", rule.Column)
		}

		if hasFill && ToExpression(rule.FillNullWith) == nil {
			return ResolvedBranch{}, nil, fmt.Errorf("unsupported fill value type %T for column %q", rule.FillNullWith, rule.Column)
		}
		if hasReplaceFrom && ToExpression(rule.ReplaceFrom) == nil {
			return ResolvedBranch{}, nil, fmt.Errorf("unsupported replace from value type %T for column %q", rule.ReplaceFrom, rule.Column)
		}
		if hasReplaceTo && ToExpression(rule.ReplaceTo) == nil {
			return ResolvedBranch{}, nil, fmt.Errorf("unsupported replace to value type %T for column %q", rule.ReplaceTo, rule.Column)
		}

		rulesByColumn[canonicalCol] = FillReplaceRule{
			Column:       canonicalCol,
			FillNullWith: rule.FillNullWith,
			ReplaceFrom:  rule.ReplaceFrom,
			ReplaceTo:    rule.ReplaceTo,
		}
	}

	resolved := ResolvedBranch{
		Table: TableSpec{
			Name:    derivedTableName,
			Columns: cols,
		},
	}

	selectFn := func(selectAst *ast.SelectStatement) {
		selectCols := make([]ast.Expression, 0, len(base.Columns))
		for _, col := range base.Columns {
			rule, ok := rulesByColumn[col.Name]
			if !ok {
				selectCols = append(selectCols, &ast.Identifier{Name: col.Name})
				continue
			}

			expr := ast.Expression(&ast.Identifier{Name: col.Name})
			if rule.ReplaceFrom != nil && rule.ReplaceTo != nil {
				expr = &ast.CaseExpression{
					WhenClauses: []ast.WhenClause{{
						Condition: &ast.BinaryExpression{
							Left:     &ast.Identifier{Name: col.Name},
							Operator: "=",
							Right:    ToExpression(rule.ReplaceFrom),
						},
						Result: ToExpression(rule.ReplaceTo),
					}},
					ElseClause: &ast.Identifier{Name: col.Name},
				}
			}

			if rule.FillNullWith != nil {
				expr = &ast.FunctionCall{
					Name:      "COALESCE",
					Arguments: []ast.Expression{expr, ToExpression(rule.FillNullWith)},
				}
			}

			selectCols = append(selectCols, expr)
		}

		selectAst.Columns = selectCols
	}

	stmt, err := BuildBackfillInsert(base.Name, resolved, selectFn)
	if err != nil {
		return ResolvedBranch{}, nil, err
	}

	return resolved, stmt, nil
}
