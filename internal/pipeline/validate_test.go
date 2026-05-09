package pipeline

import (
	"errors"
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

func TestParseSinkNodeRef(t *testing.T) {
	tests := []struct {
		name      string
		ref       string
		nodeID    string
		label     string
		wantError bool
	}{
		{name: "standard node", ref: "node", nodeID: "node"},
		{name: "branch label", ref: "node:if", nodeID: "node", label: "if"},
		{name: "case insensitive label", ref: "node:IF", nodeID: "node", label: "if"},
		{name: "trim spaces", ref: " node : Vip ", nodeID: "node", label: "vip"},
		{name: "empty", ref: "", wantError: true},
		{name: "missing node", ref: ":if", wantError: true},
		{name: "missing label", ref: "node:", wantError: true},
		{name: "too many parts", ref: "a:b:c", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeID, label, err := parseSinkNodeRef(tt.ref)
			if tt.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.nodeID, nodeID)
			require.Equal(t, tt.label, label)
		})
	}
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

func TestParseAndValidateJSON_ValidS3FileSinkTarget(t *testing.T) {
	payload := []byte(`{
		"pipeline_id": "p1",
		"version": 1,
		"nodes": [
			{"id": "src", "type": "DataSource", "config": {"format": "csv", "path": "s3://inputs/a.csv", "mode": "infer", "options": {"header": true}}}
		],
		"sinks": [{"node_id": "src", "target_table": "out", "target": {"type": "file", "format": "parquet", "path": "s3://sinks/out.parquet", "mode": "overwrite"}}]
	}`)

	spec, err := ParseAndValidateJSON(payload)
	require.NoError(t, err)
	require.Equal(t, "s3://sinks/out.parquet", spec.Sinks[0].Target.Path)
}

func TestParseAndValidateJSON_InvalidSinkTarget(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{
			name: "missing target table still invalid",
			json: `{"pipeline_id":"p","version":1,"nodes":[{"id":"src","type":"DataSource","config":{"format":"csv","path":"s3://inputs/a.csv","mode":"infer"}}],"sinks":[{"node_id":"src","target":{"type":"file","format":"csv","path":"s3://sinks/out.csv"}}]}`,
			want: "sink target_table is required",
		},
		{
			name: "invalid target format",
			json: `{"pipeline_id":"p","version":1,"nodes":[{"id":"src","type":"DataSource","config":{"format":"csv","path":"s3://inputs/a.csv","mode":"infer"}}],"sinks":[{"node_id":"src","target_table":"out","target":{"type":"file","format":"json","path":"s3://sinks/out.json"}}]}`,
			want: "sink target format must be csv or parquet",
		},
		{
			name: "duplicate target path",
			json: `{"pipeline_id":"p","version":1,"nodes":[{"id":"src","type":"DataSource","config":{"format":"csv","path":"s3://inputs/a.csv","mode":"infer"}}],"sinks":[{"node_id":"src","target_table":"out1","target":{"type":"file","format":"csv","path":"s3://sinks/out.csv"}},{"node_id":"src","target_table":"out2","target":{"type":"file","format":"csv","path":"s3://sinks/out.csv"}}]}`,
			want: "SINK_DUPLICATE_TARGET_PATH",
		},
		{
			name: "parquet source header option invalid",
			json: `{"pipeline_id":"p","version":1,"nodes":[{"id":"src","type":"DataSource","config":{"format":"parquet","path":"s3://inputs/a.parquet","mode":"infer","options":{"header":true}}}],"sinks":[{"node_id":"src","target_table":"out"}]}`,
			want: "options.header is only supported for csv format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseAndValidateJSON([]byte(tt.json))
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
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

func TestParseAndValidateJSON_SinkLabelValidationIssues(t *testing.T) {
	tests := []struct {
		name string
		json string
		code string
	}{
		{
			name: "conditional rejects unknown label",
			code: "SINK_INVALID_LABEL",
			json: `{"pipeline_id":"p","version":1,"nodes":[{"id":"src","type":"DataSource","config":{"format":"csv","path":"data/a.csv","mode":"infer"}},{"id":"cond","type":"Conditional","inputs":["src"],"config":{"rules":[{"column":"x","operation":"eq","value":1}]}}],"sinks":[{"node_id":"cond:maybe","target_table":"out"}]}`,
		},
		{
			name: "switch rejects default branch label",
			code: "SCHEMA_INVALID",
			json: `{"pipeline_id":"p","version":1,"nodes":[{"id":"src","type":"DataSource","config":{"format":"csv","path":"data/a.csv","mode":"infer"}},{"id":"sw","type":"Switch","inputs":["src"],"config":{"branches":[{"label":"default","rules":[{"column":"x","operation":"eq","value":1}]}]}}],"sinks":[{"node_id":"sw:default","target_table":"out"}]}`,
		},
		{
			name: "switch rejects duplicate labels case insensitive",
			code: "SCHEMA_INVALID",
			json: `{"pipeline_id":"p","version":1,"nodes":[{"id":"src","type":"DataSource","config":{"format":"csv","path":"data/a.csv","mode":"infer"}},{"id":"sw","type":"Switch","inputs":["src"],"config":{"branches":[{"label":"vip","rules":[{"column":"x","operation":"eq","value":1}]},{"label":"VIP","rules":[{"column":"x","operation":"eq","value":2}]}]}}],"sinks":[{"node_id":"sw:vip","target_table":"out"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseAndValidateJSON([]byte(tt.json))
			require.Error(t, err)
			var validationErr *ValidationError
			require.True(t, errors.As(err, &validationErr))
			require.Contains(t, issueCodes(validationErr), tt.code)
		})
	}
}

func issueCodes(err *ValidationError) []string {
	codes := make([]string, 0, len(err.Issues))
	for _, issue := range err.Issues {
		codes = append(codes, issue.Code)
	}
	return codes
}
