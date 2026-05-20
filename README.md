# Go Transformer

Go Transformer is a data transformation project for defining repeatable data workflows as structured pipeline specifications.

The project is built around a simple idea: data teams should be able to describe how raw files become useful outputs without turning every transformation into a one-off script. A pipeline is made from ordered transformation steps, where each step performs a clear operation such as filtering rows, selecting columns, joining datasets, aggregating values, reshaping data, or producing final outputs.

Go Transformer focuses on common tabular data workflows. It supports working with structured files, validating pipeline definitions before they are used, and applying transformations in a consistent and reusable way. This makes it useful for experimenting with data preparation flows, documenting transformation logic, and building toward a service that can manage reusable pipeline definitions.

## What It Does

- Describes data workflows as reusable pipeline definitions
- Validates pipeline structure before execution
- Applies common tabular transformations in a predictable order
- Supports branching, joining, reshaping, and aggregating data
- Produces final transformed outputs from raw input data
- Provides a foundation for managing saved pipelines and input files

## Core Concepts

### Pipeline

A pipeline is the complete description of a data workflow. It defines the input data, the transformations to apply, and the final outputs to produce.

### Node

A node is one step in a pipeline. Each node has a specific responsibility, such as reading data, filtering rows, selecting columns, joining datasets, or reshaping results.

### Input

An input is the data that enters a pipeline or transformation step. Inputs can come from raw files or from earlier nodes in the same pipeline.

### Transformation

A transformation changes data from one shape or meaning into another. Examples include cleaning values, computing new columns, grouping rows, removing duplicates, or combining datasets.

### Sink

A sink is the final output of a pipeline. It represents the transformed result that should be saved, inspected, or passed to another system.

## Example Use Cases

- Cleaning raw CSV or Parquet files before analysis
- Preparing datasets for dashboards or reporting
- Prototyping repeatable data preparation workflows
- Validating transformation definitions before running them
- Documenting how source data becomes final output data
- Managing reusable transformation definitions for future execution

## Project Status

This repository is an implementation prototype for a reusable transformation engine. It includes sample pipeline definitions, validation logic, execution logic, and API components for managing pipeline and input-file metadata.
