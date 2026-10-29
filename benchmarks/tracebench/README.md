# Configurable trace-pipeline benchmark

This benchmark decodes synthetic OTLP requests, runs real Collector processors,
computes Datadog APM statistics before sampling, applies the real probabilistic
sampler, runs sampled-stage processors, and encodes the surviving traces.

The default workload is an arbitrary public example, **not production data or a
production calibration**. Organization-specific workload specifications,
processor configurations, telemetry, profiles and comparison reports belong
outside this repository. Do not commit them here.

## Run

Requires Linux and Go 1.26. From this directory:

```sh
GOWORK=off go test ./...
GOWORK=off go test -race ./...
GOWORK=off go test -run '^$' -bench '^BenchmarkTracePipeline$' -benchmem -benchtime=256x
```

Dependencies are pinned to Collector v0.161.0, with the local OTTL implementation
and an immutable, release-compatible pdata fork providing `Map.RenameKey`.
This avoids confounding an implementation comparison with a distribution upgrade.

## Private configuration and validation

Set `TRACEBENCH_SPEC` to an external JSON file. Its schema follows `Spec`, `Class`,
`Batch` and `Stage` in [workload.go](workload.go):

- `classes` specify weights, span kinds, instrumentation scopes, attribute values,
  filler-attribute counts/lengths and spans per resource.
- `batches` specify **request weights**, request sizes, a fixed class index or
  `-1` for the class mixture, protobuf/JSON encoding and gzip.
- `bank` requests are generated with a fixed seed. Batch selection is stratified
  and shuffled to reduce mixture error. Use an iteration count divisible by
  `bank` so both variants consume the same workload rather than different prefixes.
- `trace_spans` groups consecutive input spans into a root and direct children.
  It defaults to one: every span is an independent root. Test other values when
  trace topology is unknown; topology affects APM statistics and allocations.
- `common` and `sampled` stages accept inline component configuration or an
  external YAML `file`, resolved by the Collector's file/environment providers.
- `sample_percent` controls the actual hash sampler. `stats` enables real APM
  statistics; `stats_config` accepts connector configuration.

```sh
TRACEBENCH_SPEC=/private/scenario.json GOWORK=off \
  go test -run '^TestConfiguredCensus$' -v
TRACEBENCH_SPEC=/private/scenario.json GOWORK=off \
  go test -run '^$' -bench '^BenchmarkTracePipeline$' -benchmem -benchtime=512x
```

The census checks the actual generated mix, batch sizes, wire bytes, surviving
spans and per-stage counters. Benchmark metrics include process CPU and allocated
bytes per incoming span, input wire bytes/span, sampling survival and exclusive
stage elapsed time/span, including a separate decode-time control. Setup and load
generation are excluded. Process CPU
includes background GC and APM work; do **not** add a separate GC-saving estimate.

Set `TRACEBENCH_CPU_PROFILE=/private/run.pprof` to profile only the measured
pipeline, excluding fixture generation and component construction. Use separate
unprofiled repetitions for timing comparisons.

For A/B tests, use a Go source overlay or a separate binary to change only the
implementation under test, keeping inputs and dependency versions identical.
Interleave repetitions, retain raw observations, and report regression cases.

### Optional map-lookup implementation

The default pin includes the key-replacement API but not the additional
single-return, inlineable `Map.Get` experiment. To change only that dependency,
use a temporary modfile in this directory:

```sh
cp go.mod lookup.mod
cp go.sum lookup.sum
go mod edit -modfile=lookup.mod \
  -replace=go.opentelemetry.io/collector/pdata=github.com/githubnext/opentelemetry-collector/pdata@v0.0.0-20261029211442-6eb86626581e
GOWORK=off go test -mod=mod -modfile=lookup.mod -run '^$' \
  -bench '^BenchmarkTracePipeline$' -benchtime=256x
rm lookup.mod lookup.sum
```

The source change and lookup/dependent-operation microbenchmarks are on
[`pdata/inline-map-get-v1.67`](https://github.com/githubnext/opentelemetry-collector/tree/pdata/inline-map-get-v1.67).
Neither implementation variant implies a production calibration or fleet savings.

## Limits

This is a receiver-codec/pipeline benchmark, not a complete Collector deployment.
It does not include HTTP/TLS/auth, Kubernetes API lookup/watch, routing/fanout,
network/exporter queues, Datadog trace-payload translation or internal scraping.
The output encoder is OTLP protobuf, not the Datadog exporter. APM ingestion is
measured, but background metric flush cost and cardinality need separate checks.
Requests are processed serially. The byte bank remains in the Go heap and its
trace IDs are reused between iterations; neither models a live receiver's heap
or trace cardinality. A run must finish before the generated span timestamps age
out of the APM statistics window.
Do not silently pad CPU or allocations to force a match.

Aggregate counters do not identify original attribute maps, collisions, compression,
resource grouping or unsampled span-kind composition. State inferred parameters,
test plausible alternatives and validate held-out counters. Matching an input
rate or a single mean does not establish full-fleet representativeness.
