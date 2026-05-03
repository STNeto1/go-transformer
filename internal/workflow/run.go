package workflow

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/formatter"
	"github.com/ajitpratap0/GoSQLX/pkg/sql/ast"

	"go-transformer/internal/pipeline"
	"go-transformer/internal/sqlspec"
)

var nonWord = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

type SinkResult struct {
	NodeID      string
	TargetTable string
	SourceTable string
	RowCount    int
}

func Run(db *sql.DB, spec *pipeline.Spec) ([]SinkResult, error) {
	if db == nil {
		return nil, fmt.Errorf("db is required")
	}
	if spec == nil {
		return nil, fmt.Errorf("spec is required")
	}

	ordered, depsByNode, err := topologicalOrder(spec)
	if err != nil {
		return nil, err
	}

	state := make(map[string]sqlspec.TableSpec, len(spec.Nodes))

	for _, node := range ordered {
		tableName := nodeTableName(spec.PipelineID, node.ID)
		switch node.Type {
		case pipeline.NodeTypeDataSource:
			if err := runDataSourceNode(db, node, tableName, state); err != nil {
				return nil, fmt.Errorf("node %s (%s): %w", node.ID, node.Type, err)
			}
		case pipeline.NodeTypeFilter:
			deps := depsByNode[node.ID]
			if len(deps) != 1 {
				return nil, fmt.Errorf("node %s (%s): requires exactly one input", node.ID, node.Type)
			}
			if err := runFilterNode(db, node, tableName, deps[0], state); err != nil {
				return nil, fmt.Errorf("node %s (%s): %w", node.ID, node.Type, err)
			}
		default:
			panic(fmt.Sprintf("unsupported node type: %s", node.Type))
		}
	}

	results := make([]SinkResult, 0, len(spec.Sinks))
	for _, sink := range spec.Sinks {
		table, ok := state[sink.NodeID]
		if !ok {
			return nil, fmt.Errorf("sink node %q was not materialized", sink.NodeID)
		}
		count, err := countRows(db, table.Name)
		if err != nil {
			return nil, fmt.Errorf("count sink rows for node %q: %w", sink.NodeID, err)
		}
		results = append(results, SinkResult{
			NodeID:      sink.NodeID,
			TargetTable: sink.TargetTable,
			SourceTable: table.Name,
			RowCount:    count,
		})
	}

	return results, nil
}

func runDataSourceNode(db *sql.DB, node pipeline.Node, tableName string, state map[string]sqlspec.TableSpec) error {
	var cfg pipeline.DataSourceConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return err
	}

	resolved, stmt, err := sqlspec.DeriveDataSource(tableName, sqlspec.DataSourceSpec{
		Format:  sqlspec.DataSourceFormat(cfg.Format),
		Path:    cfg.Path,
		Mode:    sqlspec.DataSourceMode(cfg.Mode),
		Columns: mapColumns(cfg.Columns),
	}, db)
	if err != nil {
		return err
	}

	if err := materialize(db, resolved.Table, stmt); err != nil {
		return err
	}

	state[node.ID] = resolved.Table
	return nil
}

func runFilterNode(db *sql.DB, node pipeline.Node, tableName string, upstreamNodeID string, state map[string]sqlspec.TableSpec) error {
	upstream, ok := state[upstreamNodeID]
	if !ok {
		return fmt.Errorf("upstream node %q table not found", upstreamNodeID)
	}

	var cfg pipeline.FilterConfig
	if err := decodeStrict(node.Config, &cfg); err != nil {
		return err
	}

	mode := sqlspec.ConditionalMode(strings.TrimSpace(cfg.Mode))
	if mode == "" {
		mode = sqlspec.ConditionalModeAll
	}

	rules := make([]sqlspec.FilterRow, 0, len(cfg.Rules))
	for _, r := range cfg.Rules {
		rules = append(rules, sqlspec.FilterRow{Column: r.Column, Operation: sqlspec.FilterOperation(r.Operation), Value: r.Value})
	}

	predicate, err := buildFilterPredicate(upstream.Columns, rules, mode)
	if err != nil {
		return err
	}

	resolved := sqlspec.ResolvedBranch{Table: sqlspec.TableSpec{Name: tableName, Columns: slices.Clone(upstream.Columns)}}
	stmt, err := sqlspec.BuildBackfillInsert(upstream.Name, resolved, func(s *ast.SelectStatement) {
		s.Where = predicate
	})
	if err != nil {
		return err
	}

	if err := materialize(db, resolved.Table, stmt); err != nil {
		return err
	}

	state[node.ID] = resolved.Table
	return nil
}

func buildFilterPredicate(cols []sqlspec.ColumnSpec, rules []sqlspec.FilterRow, mode sqlspec.ConditionalMode) (ast.Expression, error) {
	if len(rules) == 0 {
		return nil, fmt.Errorf("at least one filter rule is required")
	}

	var pred ast.Expression
	for _, rule := range rules {
		idx := slices.IndexFunc(cols, func(c sqlspec.ColumnSpec) bool { return strings.EqualFold(c.Name, rule.Column) })
		if idx < 0 {
			return nil, fmt.Errorf("column does not exist")
		}
		expr, err := buildFilterExpr(cols[idx], rule)
		if err != nil {
			return nil, err
		}
		if pred == nil {
			pred = expr
			continue
		}
		op := "AND"
		if mode == sqlspec.ConditionalModeAny {
			op = "OR"
		}
		pred = &ast.BinaryExpression{Left: pred, Operator: op, Right: expr}
	}

	return pred, nil
}

func buildFilterExpr(col sqlspec.ColumnSpec, filter sqlspec.FilterRow) (ast.Expression, error) {
	left := &ast.Identifier{Name: col.Name}

	switch filter.Operation {
	case sqlspec.FilterOperationEq, sqlspec.FilterOperationNe, sqlspec.FilterOperationGt, sqlspec.FilterOperationLt:
		rightExpr := sqlspec.ToExpression(filter.Value)
		if rightExpr == nil {
			return nil, fmt.Errorf("unsupported filter value type %T", filter.Value)
		}

		op := "="
		switch filter.Operation {
		case sqlspec.FilterOperationNe:
			op = "!="
		case sqlspec.FilterOperationGt:
			op = ">"
		case sqlspec.FilterOperationLt:
			op = "<"
		}

		return &ast.BinaryExpression{Left: left, Operator: op, Right: rightExpr}, nil

	case sqlspec.FilterOperationContains, sqlspec.FilterOperationStartsWith, sqlspec.FilterOperationEndsWith:
		v, ok := filter.Value.(string)
		if !ok {
			return nil, fmt.Errorf("operation %q requires string value", filter.Operation)
		}

		pattern := v
		switch filter.Operation {
		case sqlspec.FilterOperationContains:
			pattern = "%" + v + "%"
		case sqlspec.FilterOperationStartsWith:
			pattern = v + "%"
		case sqlspec.FilterOperationEndsWith:
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

func materialize(db *sql.DB, table sqlspec.TableSpec, stmt *ast.InsertStatement) error {
	if _, err := db.Exec("DROP TABLE IF EXISTS " + table.Name); err != nil {
		return err
	}
	createStmt, err := sqlspec.BuildCreateTable(table)
	if err != nil {
		return err
	}
	if _, err := db.Exec(formatter.FormatStatement(createStmt, ast.CompactStyle())); err != nil {
		return err
	}
	if _, err := db.Exec(formatter.FormatStatement(stmt, ast.CompactStyle())); err != nil {
		return err
	}
	return nil
}

func countRows(db *sql.DB, table string) (int, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count)
	return count, err
}

func mapColumns(cols []pipeline.ColumnSpec) []sqlspec.ColumnSpec {
	out := make([]sqlspec.ColumnSpec, 0, len(cols))
	for _, c := range cols {
		out = append(out, sqlspec.ColumnSpec{Name: c.Name, SQLType: c.SQLType})
	}
	return out
}

func nodeTableName(pipelineID string, nodeID string) string {
	base := "wf_" + sanitizeName(pipelineID) + "_" + sanitizeName(nodeID)
	return strings.ToLower(strings.Trim(base, "_"))
}

func sanitizeName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "x"
	}
	s = nonWord.ReplaceAllString(s, "_")
	return strings.Trim(s, "_")
}

func topologicalOrder(spec *pipeline.Spec) ([]pipeline.Node, map[string][]string, error) {
	nodeByID := make(map[string]pipeline.Node, len(spec.Nodes))
	depsByNode := make(map[string][]string, len(spec.Nodes))
	indegree := make(map[string]int, len(spec.Nodes))
	adj := make(map[string][]string, len(spec.Nodes))

	for _, node := range spec.Nodes {
		nodeByID[node.ID] = node
		indegree[node.ID] = 0
		adj[node.ID] = nil
	}

	for _, node := range spec.Nodes {
		deps, err := extractDeps(node)
		if err != nil {
			return nil, nil, fmt.Errorf("node %s: %w", node.ID, err)
		}
		depsByNode[node.ID] = deps
		for _, dep := range deps {
			if _, ok := nodeByID[dep]; !ok {
				return nil, nil, fmt.Errorf("node %s dependency %q does not exist", node.ID, dep)
			}
			adj[dep] = append(adj[dep], node.ID)
			indegree[node.ID]++
		}
	}

	queue := make([]string, 0, len(spec.Nodes))
	for _, node := range spec.Nodes {
		if indegree[node.ID] == 0 {
			queue = append(queue, node.ID)
		}
	}

	ordered := make([]pipeline.Node, 0, len(spec.Nodes))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		ordered = append(ordered, nodeByID[id])
		for _, next := range adj[id] {
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if len(ordered) != len(spec.Nodes) {
		return nil, nil, fmt.Errorf("cycle detected in pipeline graph")
	}

	return ordered, depsByNode, nil
}

func extractDeps(node pipeline.Node) ([]string, error) {
	if len(node.Inputs) == 0 {
		return nil, nil
	}

	var arr []string
	if err := decodeStrict(node.Inputs, &arr); err == nil {
		return arr, nil
	}

	var obj pipeline.JoinInputs
	if err := decodeStrict(node.Inputs, &obj); err == nil {
		deps := make([]string, 0, 2)
		if obj.Left != "" {
			deps = append(deps, obj.Left)
		}
		if obj.Right != "" {
			deps = append(deps, obj.Right)
		}
		return deps, nil
	}

	return nil, fmt.Errorf("unsupported inputs shape")
}

func decodeStrict(data []byte, out any) error {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("unexpected trailing content")
	}
	return nil
}
