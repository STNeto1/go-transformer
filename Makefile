ROWS ?= 1000000
SEED ?= 42
PREVIEW ?= 5
DATA_DIR ?= data
TELEMETRY_FORMAT ?= text
TELEMETRY_LEVEL ?= basic

.PHONY: help gen-data run-workloads run-transformations validate-pipeline run-pipeline demo demo-transformations test

help:
	@printf "Targets:\n"
	@printf "  make gen-data       Generate CSV + Parquet demo data\n"
	@printf "  make run-workloads  Run source-node workload scenarios\n"
	@printf "  make run-transformations Run transformation examples\n"
	@printf "  make validate-pipeline Validate pipeline JSON spec\n"
	@printf "  make run-pipeline   Execute a pipeline JSON workflow\n"
	@printf "  make demo           Generate data, then run workloads\n"
	@printf "  make demo-transformations Generate data, then run transformations\n"
	@printf "  make test           Run Go test suite\n"
	@printf "\nVariables (override with VAR=value):\n"
	@printf "  ROWS=%s\n" "$(ROWS)"
	@printf "  SEED=%s\n" "$(SEED)"
	@printf "  PREVIEW=%s\n" "$(PREVIEW)"
	@printf "  DATA_DIR=%s\n" "$(DATA_DIR)"
	@printf "  TELEMETRY_FORMAT=%s\n" "$(TELEMETRY_FORMAT)"
	@printf "  TELEMETRY_LEVEL=%s\n" "$(TELEMETRY_LEVEL)"

gen-data:
	go run ./cmd/gen-data --rows $(ROWS) --seed $(SEED) --out-dir $(DATA_DIR)

run-workloads:
	go run ./cmd/run-workloads --rows $(ROWS) --seed $(SEED) --preview $(PREVIEW) --data-dir $(DATA_DIR) --telemetry-format $(TELEMETRY_FORMAT) --telemetry-level $(TELEMETRY_LEVEL)

run-transformations:
	go run ./cmd/run-transformations --preview $(PREVIEW) --data-dir $(DATA_DIR) --telemetry-format $(TELEMETRY_FORMAT) --telemetry-level $(TELEMETRY_LEVEL)

validate-pipeline:
	@if [ -z "$(FILE)" ]; then echo "Usage: make validate-pipeline FILE=path/to/pipeline.json"; exit 1; fi
	go run ./cmd/validate-pipeline --file $(FILE)

run-pipeline:
	@if [ -z "$(FILE)" ]; then echo "Usage: make run-pipeline FILE=path/to/pipeline.json"; exit 1; fi
	go run ./cmd/run-pipeline --file $(FILE)

demo: gen-data run-workloads

demo-transformations: gen-data run-transformations

test:
	go test ./...
