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
