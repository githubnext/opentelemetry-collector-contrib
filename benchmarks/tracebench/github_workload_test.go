// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package tracebench

import (
	"reflect"
	"testing"
)

func BenchmarkGitHubTracePipeline(b *testing.B) {
	b.Setenv("TRACEBENCH_SPEC", "")
	b.Setenv("TRACEBENCH_WORKLOAD", "github")
	BenchmarkTracePipeline(b)
}

func TestGitHubSpec(t *testing.T) {
	spec, err := GitHubSpec()
	if err != nil {
		t.Fatal(err)
	}
	if spec.Bank != 96 || len(spec.Classes) != 8 || spec.SamplePercent != 1 || !spec.Stats {
		t.Fatal("GitHub workload shape changed")
	}
	for _, class := range spec.Classes {
		if len(class.Resource) != 40 || class.Resources != 128 {
			t.Fatal("GitHub resource shape changed")
		}
	}
	for _, stage := range append(spec.Common, spec.Sampled...) {
		if stage.File != "" || stage.Config == nil {
			t.Fatal("GitHub workload must be self-contained")
		}
	}
	if GitHubWorkloadVersion != "1.1.0" {
		t.Fatal("Unexpected generated workload version")
	}
	t.Setenv("TRACEBENCH_WORKLOAD", "github")
	selected, err := ReadSpec("")
	if err != nil || !reflect.DeepEqual(selected, spec) {
		t.Fatalf("Built-in selection differs: %v", err)
	}
	t.Setenv("TRACEBENCH_WORKLOAD", "unknown")
	if _, err := ReadSpec(""); err == nil {
		t.Fatal("Unknown built-in workload must fail")
	}
}
