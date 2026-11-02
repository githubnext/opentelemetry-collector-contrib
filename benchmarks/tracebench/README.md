# Configurable trace-pipeline benchmark

This benchmark decodes synthetic OTLP requests, runs real Collector processors,
computes Datadog APM statistics before sampling, applies the real probabilistic
sampler, runs sampled-stage processors, and encodes the surviving traces.

The default workload is an arbitrary public example, **not production data or a
production calibration**. A self-contained, production-informed synthetic
scenario is checked in as [generated Go code](github_workload_generated.go), described below.
Raw organization-specific telemetry, internal processor configurations and
profiles belong outside this repository. Do not commit them here.

## Public GitHub-shaped workload

The workload and runner share version **1.1.0**, stored in [VERSION](VERSION).
Fetch the immutable `tracebench-v1.1.0` tag from
https://github.com/githubnext/opentelemetry-collector-contrib and check it out:

```sh
git clone --branch tracebench-v1.1.0 --single-branch \
  https://github.com/githubnext/opentelemetry-collector-contrib.git /tmp/tracebench-v1.1.0
cd /tmp/tracebench-v1.1.0/benchmarks/tracebench
```

```sh
GOTOOLCHAIN=go1.26.0 python3 compare.py --candidate batched-tags \
  --repository /path/to/datadog-agent \
  --base 83b6d6e01408fb131211a8f10678f83ef7d64dc9 \
  --head 384e8e598bb6c40446a4c73e835260c340c72468 \
  --workload github --pgo --blocks 16 --iterations 192 \
  --output /tmp/tracebench-github-shaped
```

Fetch the candidate commit into the Datadog checkout first, as described below.
The output directory must not already exist. The checked-in Go workload contains
all required processor configuration inline; no generation, private files or
credentials are needed. The runner selects it directly with `--workload github`
and uses the same baseline-trained PGO profile on both sides. Results record the
shared version and selected workload.

To run the Go benchmark directly:

```sh
GOTOOLCHAIN=go1.26.0 GOWORK=off GOMAXPROCS=1 \
  go test -run '^$' -bench '^BenchmarkGitHubTracePipeline$' -benchmem -benchtime=192x
```

For maintainers only, regenerate the checked-in source after changing the model:
`python3 github_workload.py --go > github_workload_generated.go`, then `gofmt`.
Python tests verify that the checked-in Go data and shared version match the
generator. External scenarios remain available through `--spec`.

This scenario carries forward rounded workload parameters fitted during a
Collector performance investigation at GitHub: a predominantly internal/client
SDK mixture with a small mesh fraction, roughly 554 spans per batch, protobuf
without compression, and approximately one-percent sampling. Flow proportions
were informed by receiver, processor and pre-sampling APM counters; batching was
informed by processor call counts and bounded request-shape observations.
Actual values, identities, instrumentation payloads and internal processing rules
are replaced with synthetic examples. No raw telemetry, internal hostnames,
service inventories, configuration snapshots or measured fleet totals are included.

The resource-map shape is a hypothesis, not an independently witnessed input
census: 40 initial resource attributes include 30 synthetic filler keys, and
three public example metadata attributes are added before statistics. Attribute
counts, filler lengths and resource grouping were informed by retained-tag/wire
shape priors. Retained tags have already been processed and do not identify
original OTLP maps. Internal-span share is an ingress residual rather than a
directly observed kind count. Missing APM series were not validated as zeros.
All spans are independent roots; topology, cardinality, concurrency, live heap
and exporter overhead are not calibrated.

The public transforms illustrate normalization, peer-service extraction and
sampled filtering, rather than reproducing GitHub's internal pipeline. Therefore
this is a sanitized derivative of the earlier fitted workload, not its exact
historical measurement fixture. Its results must be measured independently;
historical private-workload percentages must not be assigned to this version.
Change filler counts, grouping or `trace_spans` to test sensitivity; do not assume
the model uniquely describes production or that its CPU percentage transfers.

Version 1.1.0 embeds exactly the same workload data as version 1.0.0; only selection
and packaging changed. An independent 16 A/B + 16 A/A campaign of that workload and the batched-tag
candidate above (Go 1.26.0, PGO, 192 iterations/process, 128 observations) measured
11.17% less pipeline CPU, with conditional 95% interval 7.56–13.76%, and allocated
bytes +0.052%. Its same-binary A/A apparent reduction was 1.44%, with interval
−1.49–4.72%. These intervals describe variation on one shared host, not a
production guarantee or an A/A-adjusted estimate. Both workload/stage/sampling
censuses matched. The earlier private-fixture result is not substituted here.

## Run

Requires Linux and Go 1.26. From this directory:

```sh
GOWORK=off go test ./...
GOWORK=off go test -race ./...
GOWORK=off go test -run '^$' -bench '^BenchmarkTracePipeline$' -benchmem -benchtime=256x
```

Dependencies are pinned to Collector v0.164.1 components and official pdata,
with the local OTTL implementation.
This avoids confounding an implementation comparison with a distribution upgrade.
The standalone benchmark module is excluded from release module sets to retain
those comparison pins; the repository's module discovery still includes it.

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

## Reproduce an individual source improvement

Source patches retain surrounding context and must apply cleanly to the pinned
dependencies. Zero-context patches are unsafe across source versions: an
insertion can land at the wrong line and change control flow while still
compiling. An incompatible patch fails explicitly instead.

[compare.py](compare.py) uses Python 3's standard library, Git and Go. Run it
from this directory, with `--output` outside the source tree. It copies the
baseline dependencies before applying only the named production-file diff,
without editing the shared Go module cache or upgrading dependencies.

It builds separate binaries, checks both workload/stage/survival censuses,
warms up both variants and runs shuffled A/B and A/A phases with alternating
ABBA/BAAB blocks. The optional `--pgo` flag first trains on the unchanged
baseline and then uses **that same public profile for both binaries**. No CPU
profiling occurs during the measured comparison.

Fetch the relevant fork's source branch into a local checkout first:

```sh
git -C /path/to/opentelemetry-collector fetch \
  https://github.com/githubnext/opentelemetry-collector.git \
  oss/pdata-map-get:refs/remotes/githubnext/oss/pdata-map-get
git -C /path/to/opentelemetry-collector-contrib fetch \
  https://github.com/githubnext/opentelemetry-collector-contrib.git \
  oss/ottl-literal-regex:refs/remotes/githubnext/oss/ottl-literal-regex
git -C /path/to/datadog-agent fetch \
  https://github.com/githubnext/datadog-agent.git \
  oss/kind-aware-operation-name:refs/remotes/githubnext/oss/kind-aware-operation-name \
  oss/batched-span-attribute-tags:refs/remotes/githubnext/oss/batched-span-attribute-tags
```

Each comparison is independent; do not stack patches or add their percentages:

```sh
python3 compare.py --candidate map-get \
  --repository /path/to/opentelemetry-collector \
  --base 302c933caa8c66f7ebf35ea42238b261bc3e5fc6 \
  --head refs/remotes/githubnext/oss/pdata-map-get \
  --pgo --blocks 8 --iterations 256 --output /tmp/tracebench-map-get

python3 compare.py --candidate literal-regex \
  --repository /path/to/opentelemetry-collector-contrib \
  --base a1cd60723506d875a7a4a94b525b9615c11f08fb \
  --head refs/remotes/githubnext/oss/ottl-literal-regex \
  --pgo --blocks 8 --iterations 256 --output /tmp/tracebench-literal-regex

python3 compare.py --candidate kind-aware \
  --repository /path/to/datadog-agent \
  --base 83b6d6e01408fb131211a8f10678f83ef7d64dc9 \
  --head refs/remotes/githubnext/oss/kind-aware-operation-name \
  --pgo --blocks 8 --iterations 256 --output /tmp/tracebench-kind-aware

python3 compare.py --candidate batched-tags \
  --repository /path/to/datadog-agent \
  --base 83b6d6e01408fb131211a8f10678f83ef7d64dc9 \
  --head refs/remotes/githubnext/oss/batched-span-attribute-tags \
  --pgo --blocks 8 --iterations 256 --output /tmp/tracebench-batched-tags
```

Each new output directory retains source patches/hashes, build logs, module
graphs, public training profile, census, raw observations and `result.json`.
Positive `less_cpu_percent` means less process CPU. Positive
`allocated_bytes_change_percent` means more allocated bytes. Intervals use
paired geometric block ratios and a circular block bootstrap of length four
with 5,000 draws. A/A intervals diagnose same-binary host variability; the
script does not subtract that variability or certify acceptance.

The public example exercises literal-key rewriting, Map.Get, operation naming
and APM statistics. Its client-only composition may give little benefit for
kind-aware naming, which skips irrelevant families for other kinds. Extend the
workload using the documented schema and publish the exact non-sensitive
scenario if claiming an effect. Add `--spec /path/to/scenario.json` to run an
external calibration; use an iteration count divisible by its `bank`.

**No fixed percentage or typical-workflow benefit is promised.** Results depend
on span kinds, attribute maps, stage configuration, compiler/PGO and hardware.
The arbitrary public example is not an internal production calibration. Census
equivalence covers input composition, counters and survivors, not every output
attribute or derived APM metric. Run the source proposal's correctness tests
as well. Repeat on a quiet host, report the workload and controls, and compare
PGO-on before evaluating an additional source improvement.

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
