package pipeline

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAndValidateJSON_ValidMultiSourceDAG(t *testing.T) {
	payload := []byte(`{
		"pipeline_id": "sales_customer_rollup_v1",
		"version": 1,
		"nodes": [
			{
				"id": "src_orders",
				"type": "DataSource",
				"config": {
					"format": "parquet",
					"path": "data/orders.parquet",
					"mode": "infer"
				}
			},
			{
				"id": "src_customers",
				"type": "DataSource",
				"config": {
					"format": "csv",
					"path": "data/customers.csv",
					"mode": "declared",
					"columns": [
						{"name": "id", "sql_type": "BIGINT"},
						{"name": "country", "sql_type": "VARCHAR"}
					]
				}
			},
			{
				"id": "orders_paid",
				"type": "Filter",
				"inputs": ["src_orders"],
				"config": {
					"mode": "all",
					"rules": [{"column": "status", "operation": "eq", "value": "paid"}]
				}
			},
			{
				"id": "orders_with_customer",
				"type": "Join",
				"inputs": {"left": "orders_paid", "right": "src_customers"},
				"config": {
					"mode": "left",
					"keys": [{"left": "customer_id", "right": "id"}]
				}
			},
			{
				"id": "country_rollup",
				"type": "Aggregate",
				"inputs": ["orders_with_customer"],
				"config": {
					"group_by": ["country"],
					"metrics": [
						{"function": "count", "column": "*", "as": "orders_count"},
						{"function": "sum", "column": "amount", "as": "gross_amount"}
					]
				}
			}
		],
		"sinks": [{"node_id": "country_rollup", "target_table": "country_sales_rollup"}]
	}`)

	spec, err := ParseAndValidateJSON(payload)
	require.NoError(t, err)
	require.Equal(t, "sales_customer_rollup_v1", spec.PipelineID)
	require.Len(t, spec.Nodes, 5)
}

func TestParseAndValidateJSON_InvalidJSON(t *testing.T) {
	_, err := ParseAndValidateJSON([]byte(`{"pipeline_id":`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "INVALID_JSON")
}

func TestParseAndValidateJSON_DetectsUnknownDependency(t *testing.T) {
	payload := []byte(`{
		"pipeline_id": "p1",
		"version": 1,
		"nodes": [
			{"id": "src", "type": "DataSource", "config": {"format": "csv", "path": "data/a.csv", "mode": "infer"}},
			{"id": "agg", "type": "Aggregate", "inputs": ["missing"], "config": {"group_by": ["country"], "metrics": [{"function": "count", "column": "*", "as": "c"}]}}
		],
		"sinks": [{"node_id": "agg", "target_table": "out"}]
	}`)

	_, err := ParseAndValidateJSON(payload)
	require.Error(t, err)
	require.Contains(t, err.Error(), "NODE_MISSING_DEPENDENCY")
}

func TestParseAndValidateJSON_DetectsCycle(t *testing.T) {
	payload := []byte(`{
		"pipeline_id": "p1",
		"version": 1,
		"nodes": [
			{"id": "a", "type": "Filter", "inputs": ["b"], "config": {"rules": [{"column": "x", "operation": "eq", "value": 1}]}},
			{"id": "b", "type": "Filter", "inputs": ["a"], "config": {"rules": [{"column": "x", "operation": "eq", "value": 1}]}}
		],
		"sinks": [{"node_id": "a", "target_table": "out"}]
	}`)

	_, err := ParseAndValidateJSON(payload)
	require.Error(t, err)
	require.Contains(t, err.Error(), "GRAPH_CYCLE")
}

func TestParseAndValidateJSON_JoinNeedsNamedInputs(t *testing.T) {
	payload := []byte(`{
		"pipeline_id": "p1",
		"version": 1,
		"nodes": [
			{"id": "l", "type": "DataSource", "config": {"format": "csv", "path": "data/l.csv", "mode": "infer"}},
			{"id": "r", "type": "DataSource", "config": {"format": "csv", "path": "data/r.csv", "mode": "infer"}},
			{"id": "j", "type": "Join", "inputs": ["l", "r"], "config": {"mode": "inner", "keys": [{"left": "id", "right": "id"}]}}
		],
		"sinks": [{"node_id": "j", "target_table": "out"}]
	}`)

	_, err := ParseAndValidateJSON(payload)
	require.Error(t, err)
	require.Contains(t, err.Error(), "join inputs must be an object with left/right")
}

func TestParseAndValidateJSON_ValidSelectSortLimitAndSinkLabel(t *testing.T) {
	payload := []byte(`{
		"pipeline_id": "p2",
		"version": 1,
		"nodes": [
			{"id": "src", "type": "DataSource", "config": {"format": "csv", "path": "data/people.csv", "mode": "infer"}},
			{"id": "sel", "type": "SelectColumns", "inputs": ["src"], "config": {"columns": ["id", "age", "country"]}},
			{"id": "sort", "type": "Sort", "inputs": ["sel"], "config": {"keys": [{"column": "age", "direction": "desc", "nulls": "last"}]}},
			{"id": "lim", "type": "Limit", "inputs": ["sort"], "config": {"count": 10}}
		],
		"sinks": [{"node_id": "lim", "target_table": "out"}]
	}`)

	_, err := ParseAndValidateJSON(payload)
	require.NoError(t, err)
}

func TestParseAndValidateJSON_ValidConditionalAndSwitchSinkLabels(t *testing.T) {
	payload := []byte(`{
		"pipeline_id": "p3",
		"version": 1,
		"nodes": [
			{"id": "src", "type": "DataSource", "config": {"format": "csv", "path": "data/people.csv", "mode": "infer"}},
			{"id": "cond", "type": "Conditional", "inputs": ["src"], "config": {"mode": "all", "rules": [{"column": "age", "operation": "gt", "value": 30}]}},
			{"id": "sw", "type": "Switch", "inputs": ["src"], "config": {"branches": [
				{"label": "vip", "mode": "all", "rules": [{"column": "country", "operation": "eq", "value": "US"}]},
				{"label": "regular", "mode": "any", "rules": [{"column": "country", "operation": "eq", "value": "BR"}]}
			]}}
		],
		"sinks": [
			{"node_id": "cond:IF", "target_table": "cond_if"},
			{"node_id": "cond:else", "target_table": "cond_else"},
			{"node_id": "sw:DEFAULT", "target_table": "sw_default"},
			{"node_id": "sw:Vip", "target_table": "sw_vip"}
		]
	}`)

	_, err := ParseAndValidateJSON(payload)
	require.NoError(t, err)
}

func TestParseAndValidateJSON_InvalidSinkLabelForNonBranchNode(t *testing.T) {
	payload := []byte(`{
		"pipeline_id": "p4",
		"version": 1,
		"nodes": [
			{"id": "src", "type": "DataSource", "config": {"format": "csv", "path": "data/people.csv", "mode": "infer"}}
		],
		"sinks": [{"node_id": "src:if", "target_table": "out"}]
	}`)

	_, err := ParseAndValidateJSON(payload)
	require.Error(t, err)
	require.Contains(t, err.Error(), "SINK_INVALID_LABEL")
}

func TestParseAndValidateJSON_InvalidSwitchLabel(t *testing.T) {
	payload := []byte(`{
		"pipeline_id": "p5",
		"version": 1,
		"nodes": [
			{"id": "src", "type": "DataSource", "config": {"format": "csv", "path": "data/people.csv", "mode": "infer"}},
			{"id": "sw", "type": "Switch", "inputs": ["src"], "config": {"branches": [
				{"label": "vip", "rules": [{"column": "country", "operation": "eq", "value": "US"}]}
			]}}
		],
		"sinks": [{"node_id": "sw:gold", "target_table": "out"}]
	}`)

	_, err := ParseAndValidateJSON(payload)
	require.Error(t, err)
	require.Contains(t, err.Error(), "SINK_INVALID_LABEL")
}
