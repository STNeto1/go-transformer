package sqlspec

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"
)

type AggregateFunction string

const (
	AggregateFunctionCount AggregateFunction = "count"
	AggregateFunctionSum   AggregateFunction = "sum"
	AggregateFunctionAvg   AggregateFunction = "avg"
	AggregateFunctionMin   AggregateFunction = "min"
	AggregateFunctionMax   AggregateFunction = "max"
)

type AggregateMetric struct {
	Function AggregateFunction
	Column   string
	As       string
}

type AggregateSpec struct {
	GroupBy []string
	Metrics []AggregateMetric
}

func DeriveAggregate(base TableSpec, derivedTableName string, spec AggregateSpec) (ResolvedBranch, *ast.InsertStatement, error) {
	if base.Name == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("base table name is required")
	}
	if derivedTableName == "" {
		return ResolvedBranch{}, nil, fmt.Errorf("derived table name is required")
	}
	if len(spec.Metrics) == 0 {
		return ResolvedBranch{}, nil, fmt.Errorf("at least one aggregate metric is required")
	}

	groupCols, groupExprs, err := resolveGroupBy(base.Columns, spec.GroupBy)
	if err != nil {
		return ResolvedBranch{}, nil, err
	}

	metricCols := make([]ColumnSpec, 0, len(spec.Metrics))
	metricExprs := make([]ast.Expression, 0, len(spec.Metrics))
	metricAliasesSeen := map[string]struct{}{}
	groupByNamesSeen := map[string]struct{}{}
	for _, c := range groupCols {
		groupByNamesSeen[strings.ToLower(c.Name)] = struct{}{}
	}

	for _, metric := range spec.Metrics {
		if metric.As == "" {
			return ResolvedBranch{}, nil, fmt.Errorf("aggregate metric alias is required")
		}

		aliasKey := strings.ToLower(metric.As)
		if _, exists := metricAliasesSeen[aliasKey]; exists {
			return ResolvedBranch{}, nil, fmt.Errorf("duplicate aggregate alias %q", metric.As)
		}
		if _, collides := groupByNamesSeen[aliasKey]; collides {
			return ResolvedBranch{}, nil, fmt.Errorf("aggregate alias %q collides with group by column", metric.As)
		}
		metricAliasesSeen[aliasKey] = struct{}{}

		fnName, sqlType, argExpr, err := resolveAggregateMetric(base.Columns, metric)
		if err != nil {
			return ResolvedBranch{}, nil, err
		}

		metricExprs = append(metricExprs, &ast.FunctionCall{
			Name:      fnName,
			Arguments: []ast.Expression{argExpr},
		})
		metricCols = append(metricCols, ColumnSpec{Name: metric.As, SQLType: sqlType})
	}

	resolvedCols := make([]ColumnSpec, 0, len(groupCols)+len(metricCols))
	resolvedCols = append(resolvedCols, groupCols...)
	resolvedCols = append(resolvedCols, metricCols...)

	resolved := ResolvedBranch{Table: TableSpec{Name: derivedTableName, Columns: resolvedCols}}

	selectFn := func(selectAst *ast.SelectStatement) {
		selectAst.Columns = make([]ast.Expression, 0, len(groupExprs)+len(metricExprs))
		selectAst.Columns = append(selectAst.Columns, groupExprs...)
		selectAst.Columns = append(selectAst.Columns, metricExprs...)
		selectAst.GroupBy = groupExprs
	}

	stmt, err := BuildBackfillInsert(base.Name, resolved, selectFn)
	if err != nil {
		return ResolvedBranch{}, nil, err
	}

	return resolved, stmt, nil
}

func resolveGroupBy(baseCols []ColumnSpec, groupBy []string) ([]ColumnSpec, []ast.Expression, error) {
	if len(groupBy) == 0 {
		return nil, nil, nil
	}

	resolved := make([]ColumnSpec, 0, len(groupBy))
	exprs := make([]ast.Expression, 0, len(groupBy))
	seen := map[string]struct{}{}

	for _, col := range groupBy {
		if col == "" {
			return nil, nil, fmt.Errorf("group by column is required")
		}
		idx := slices.IndexFunc(baseCols, func(c ColumnSpec) bool {
			return strings.EqualFold(c.Name, col)
		})
		if idx == -1 {
			return nil, nil, fmt.Errorf("column %q does not exist", col)
		}
		canonical := baseCols[idx]
		key := strings.ToLower(canonical.Name)
		if _, exists := seen[key]; exists {
			return nil, nil, fmt.Errorf("duplicate group by column %q", col)
		}
		seen[key] = struct{}{}
		resolved = append(resolved, canonical)
		exprs = append(exprs, &ast.Identifier{Name: canonical.Name})
	}

	return resolved, exprs, nil
}

func resolveAggregateMetric(baseCols []ColumnSpec, metric AggregateMetric) (string, string, ast.Expression, error) {
	fn := strings.ToLower(string(metric.Function))
	if fn == "" {
		return "", "", nil, fmt.Errorf("aggregate function is required")
	}

	switch AggregateFunction(fn) {
	case AggregateFunctionCount:
		if metric.Column == "" || metric.Column == "*" {
			return "COUNT", "bigint", &ast.LiteralValue{Value: 1, Type: "INTEGER"}, nil
		}
		idx := slices.IndexFunc(baseCols, func(c ColumnSpec) bool {
			return strings.EqualFold(c.Name, metric.Column)
		})
		if idx == -1 {
			return "", "", nil, fmt.Errorf("column %q does not exist", metric.Column)
		}
		return "COUNT", "bigint", &ast.Identifier{Name: baseCols[idx].Name}, nil
	case AggregateFunctionSum, AggregateFunctionAvg, AggregateFunctionMin, AggregateFunctionMax:
		if metric.Column == "" {
			return "", "", nil, fmt.Errorf("aggregate column is required for %q", metric.Function)
		}
		idx := slices.IndexFunc(baseCols, func(c ColumnSpec) bool {
			return strings.EqualFold(c.Name, metric.Column)
		})
		if idx == -1 {
			return "", "", nil, fmt.Errorf("column %q does not exist", metric.Column)
		}
		col := baseCols[idx]
		outType := col.SQLType
		if AggregateFunction(fn) == AggregateFunctionAvg {
			outType = "double"
		}
		if outType == "" {
			outType = "double"
		}
		return strings.ToUpper(fn), outType, &ast.Identifier{Name: col.Name}, nil
	default:
		return "", "", nil, fmt.Errorf("unsupported aggregate function %q", metric.Function)
	}
}
