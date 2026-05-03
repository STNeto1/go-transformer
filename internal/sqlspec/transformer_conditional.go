package sqlspec

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

type ConditionalMode string

const (
	ConditionalModeAll ConditionalMode = "all"
	ConditionalModeAny ConditionalMode = "any"
)

type ConditionalSpec struct {
	Mode  ConditionalMode
	Rules []FilterRow
}

type ConditionalResult struct {
	IfBranch     ResolvedBranch
	IfBackfill   *ast.InsertStatement
	ElseBranch   ResolvedBranch
	ElseBackfill *ast.InsertStatement
}

func DeriveConditional(base TableSpec, ifTableName string, elseTableName string, spec ConditionalSpec) (ConditionalResult, error) {
	if base.Name == "" {
		return ConditionalResult{}, fmt.Errorf("base table name is required")
	}
	if ifTableName == "" {
		return ConditionalResult{}, fmt.Errorf("if table name is required")
	}
	if elseTableName == "" {
		return ConditionalResult{}, fmt.Errorf("else table name is required")
	}
	if strings.EqualFold(ifTableName, elseTableName) {
		return ConditionalResult{}, fmt.Errorf("if and else table names must be different")
	}

	mode := spec.Mode
	if mode == "" {
		mode = ConditionalModeAll
	}
	if mode != ConditionalModeAll && mode != ConditionalModeAny {
		return ConditionalResult{}, fmt.Errorf("unsupported conditional mode %q", mode)
	}

	cols := make([]ColumnSpec, len(base.Columns))
	copy(cols, base.Columns)

	ifBranch := ResolvedBranch{Table: TableSpec{Name: ifTableName, Columns: cols}}
	elseBranch := ResolvedBranch{Table: TableSpec{Name: elseTableName, Columns: cols}}

	if len(spec.Rules) == 0 {
		ifBackfill, err := BuildBackfillInsert(base.Name, ifBranch)
		if err != nil {
			return ConditionalResult{}, err
		}

		elseBackfill, err := BuildBackfillInsert(base.Name, elseBranch, func(s *ast.SelectStatement) {
			s.Where = &ast.BinaryExpression{
				Left:     &ast.LiteralValue{Value: 1, Type: "INTEGER"},
				Operator: "=",
				Right:    &ast.LiteralValue{Value: 0, Type: "INTEGER"},
			}
		})
		if err != nil {
			return ConditionalResult{}, err
		}

		return ConditionalResult{IfBranch: ifBranch, IfBackfill: ifBackfill, ElseBranch: elseBranch, ElseBackfill: elseBackfill}, nil
	}

	predicate, err := buildConditionalPredicate(base.Columns, spec.Rules, mode)
	if err != nil {
		return ConditionalResult{}, err
	}

	ifBackfill, err := BuildBackfillInsert(base.Name, ifBranch, func(s *ast.SelectStatement) {
		s.Where = predicate
	})
	if err != nil {
		return ConditionalResult{}, err
	}

	elsePredicate, err := buildConditionalElsePredicate(base.Columns, spec.Rules, mode)
	if err != nil {
		return ConditionalResult{}, err
	}

	elseBackfill, err := BuildBackfillInsert(base.Name, elseBranch, func(s *ast.SelectStatement) {
		s.Where = elsePredicate
	})
	if err != nil {
		return ConditionalResult{}, err
	}

	return ConditionalResult{IfBranch: ifBranch, IfBackfill: ifBackfill, ElseBranch: elseBranch, ElseBackfill: elseBackfill}, nil
}

func buildConditionalElsePredicate(baseCols []ColumnSpec, rules []FilterRow, mode ConditionalMode) (ast.Expression, error) {
	negatedRules := make([]ast.Expression, 0, len(rules))
	for _, rule := range rules {
		idx := slices.IndexFunc(baseCols, func(col ColumnSpec) bool {
			return strings.EqualFold(col.Name, rule.Column)
		})
		if idx == -1 {
			return nil, fmt.Errorf("column does not exist")
		}
		ruleExpr, err := buildFilterWhereExpr(baseCols[idx], rule)
		if err != nil {
			return nil, err
		}
		negatedRules = append(negatedRules, &ast.UnaryExpression{Operator: ast.Not, Expr: ruleExpr})
	}

	if len(negatedRules) == 0 {
		return &ast.BinaryExpression{Left: &ast.LiteralValue{Value: 1, Type: "INTEGER"}, Operator: "=", Right: &ast.LiteralValue{Value: 0, Type: "INTEGER"}}, nil
	}

	op := "OR"
	if mode == ConditionalModeAny {
		op = "AND"
	}

	result := negatedRules[0]
	for i := 1; i < len(negatedRules); i++ {
		result = &ast.BinaryExpression{Left: result, Operator: op, Right: negatedRules[i]}
	}
	return result, nil
}

func buildConditionalPredicate(baseCols []ColumnSpec, rules []FilterRow, mode ConditionalMode) (ast.Expression, error) {
	var predicate ast.Expression
	for _, rule := range rules {
		if rule.Column == "" {
			return nil, fmt.Errorf("filter column is required")
		}

		idx := slices.IndexFunc(baseCols, func(col ColumnSpec) bool {
			return strings.EqualFold(col.Name, rule.Column)
		})
		if idx == -1 {
			return nil, fmt.Errorf("column does not exist")
		}

		whereExpr, err := buildFilterWhereExpr(baseCols[idx], rule)
		if err != nil {
			return nil, err
		}

		if predicate == nil {
			predicate = whereExpr
			continue
		}

		op := "AND"
		if mode == ConditionalModeAny {
			op = "OR"
		}
		predicate = &ast.BinaryExpression{Left: predicate, Operator: op, Right: whereExpr}
	}

	return predicate, nil
}
