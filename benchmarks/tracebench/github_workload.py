#!/usr/bin/env python3
"""Emit a public, synthetic version of a production-informed Collector workload."""
import argparse
import json
from pathlib import Path
import sys

VERSION = Path(__file__).with_name("VERSION").read_text().strip()


def workload():
    resource = {
        "service.name": "example-application",
        "service.version": "example-version",
        "telemetry.sdk.language": "ruby",
        "deployment.environment": "example",
        "host.name": "example-host",
        "k8s.node.name": "example-node",
        "k8s.namespace.name": "example-namespace",
        "k8s.pod.name": "example-pod",
        "container.id": "example-container",
        "cloud.region": "example-region",
    }
    resource.update({f"synthetic.resource.{i}": f"synthetic-resource-value-{i}"
                     for i in range(30)})

    def cls(weight, kind, scope, name, attributes, extra=5):
        return {
            "weight": weight, "kind": kind, "scope": scope, "name": name,
            "attributes": attributes, "resource": dict(resource),
            "extra_attributes": extra, "value_bytes": 24, "spans_per_resource": 128,
        }

    mesh_attributes = {
        "istio.canonical_service": "example",
        "http.method": "GET", "http.status_code": 200, "http.protocol": "HTTP/2",
        "downstream_cluster": "example", "guid:x-request-id": "example-id",
        "http.host": "example.invalid", "node_id": "example",
        "peer.address": "127.0.0.1", "request_size": 128, "response_size": 256,
        "response_flags": "-", "user_agent": "example", "upstream_cluster.name": "example",
    }
    classes = [
        cls(0.6862, 1, "OpenTelemetry::Instrumentation::ActiveRecord", "internal operation", {}),
        cls(0.1693, 3, "OpenTelemetry::Instrumentation::Mysql2", "SELECT example",
            {"db.system": "mysql", "db.name": "example", "db.statement": "SELECT field FROM example WHERE id = 1"}),
        cls(0.0720, 3, "OpenTelemetry::Instrumentation::Net::HTTP", "GET",
            {"http.method": "GET", "http.status_code": 200, "server.address": "example.invalid"}),
        cls(0.0556, 2, "OpenTelemetry::Instrumentation::Rack", "GET /example",
            {"http.method": "GET", "http.status_code": 200, "http.route": "/example"}),
        cls(0.0064, 4, "example.messaging", "publish",
            {"messaging.system": "kafka", "messaging.destination": "example"}),
        cls(0.0105, 5, "example.messaging", "consume",
            {"messaging.system": "kafka", "messaging.destination": "example"}),
        cls(0, 2, "envoy", "example.invalid:443/*", dict(mesh_attributes), 0),
        cls(0, 3, "envoy", "example.invalid:443/*", dict(mesh_attributes), 0),
    ]

    def transform(name, context, statements):
        return {"name": name, "type": "transform", "config": {
            "error_mode": "propagate",
            "trace_statements": [{"context": context, "statements": statements}],
        }}

    rewrites = {
        "http.method": "http.request.method",
        "http.status_code": "http.response.status_code",
        "http.protocol": "network.protocol.version",
        "http.host": "server.address",
        "peer.address": "network.peer.address",
        "request_size": "http.request.body.size",
        "response_size": "http.response.body.size",
        "user_agent": "user_agent.original",
        "node_id": "example.node_id",
        "downstream_cluster": "example.downstream_cluster",
        "upstream_cluster.name": "example.upstream_cluster",
    }
    statements = []
    for old, new in rewrites.items():
        pattern = json.dumps(old.replace(".", "\\."))
        statements.append(
            f'replace_all_patterns(attributes, "key", {pattern}, {json.dumps(new)})')
    return {
        "seed": 2026, "bank": 96, "trace_spans": 1,
        "classes": classes,
        # These are request weights, not span proportions.
        "batches": [
            {"weight": 0.928, "size": 577, "class": -1, "format": "proto", "gzip": False},
            {"weight": 0.040, "size": 256, "class": 6, "format": "proto", "gzip": False},
            {"weight": 0.032, "size": 256, "class": 7, "format": "proto", "gzip": False},
        ],
        "sample_percent": 1, "stats": True,
        "stats_config": {"traces": {"compute_stats_by_span_kind": True, "peer_tags_aggregation": True}},
        "common": [
            transform("normalize", "span", statements),
            transform("peer-service", "span", [
                'set(attributes["peer.service"], attributes["server.address"]) where attributes["server.address"] != nil',
                'set(attributes["peer.service"], attributes["db.name"]) where attributes["db.name"] != nil',
                'set(attributes["peer.service"], attributes["messaging.destination"]) where attributes["messaging.destination"] != nil',
            ]),
            transform("resource-metadata", "resource", [
                'set(attributes["example.collector"], "synthetic")',
                'set(attributes["example.runtime"], "synthetic")',
                'set(attributes["example.platform"], "synthetic")',
            ]),
        ],
        "sampled": [
            transform("remove-sensitive-example", "span", [
                'delete_key(attributes, "db.statement")',
                'delete_key(attributes, "guid:x-request-id")',
            ]),
            transform("sampled-resource-filter", "resource", [
                'keep_keys(attributes, ["service.name", "service.version", "deployment.environment", "host.name"])',
            ]),
        ],
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", action="version", version=VERSION)
    parser.add_argument("--go", action="store_true", help="Emit the checked-in Go workload")
    args = parser.parse_args()
    if args.go:
        data = json.dumps(workload(), indent=2)
        if "`" in data:
            raise ValueError("Go raw-string workload must not contain backticks")
        sys.stdout.write(
            "// Copyright The OpenTelemetry Authors\n"
            "// SPDX-License-Identifier: Apache-2.0\n\n"
            "// Code generated by github_workload.py --go; DO NOT EDIT.\n\n"
            "package tracebench\n\n"
            'import "encoding/json"\n\n'
            f'const GitHubWorkloadVersion = "{VERSION}"\n\n'
            "func GitHubSpec() (Spec, error) {\n"
            "\tvar spec Spec\n"
            "\tif err := json.Unmarshal([]byte(githubWorkloadJSON), &spec); err != nil {\n"
            "\t\treturn Spec{}, err\n"
            "\t}\n"
            "\treturn spec, spec.Validate()\n"
            "}\n\n"
            f"const githubWorkloadJSON = `{data}`\n")
    else:
        json.dump(workload(), sys.stdout, indent=2)
        sys.stdout.write("\n")
