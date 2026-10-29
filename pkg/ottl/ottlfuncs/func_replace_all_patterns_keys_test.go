// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ottlfuncs

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl"
)

func keyReplacementGetter(tb testing.TB, value string, literal bool) ottl.StringGetter[pcommon.Map] {
	tb.Helper()
	getter, err := ottl.NewTestingLiteralGetter[pcommon.Map, string](literal, ottl.StandardStringGetter[pcommon.Map]{
		Getter: func(context.Context, pcommon.Map) (any, error) { return value, nil },
	})
	require.NoError(tb, err)
	return getter
}

func keyReplacementTarget() ottl.PMapGetSetter[pcommon.Map] {
	return ottl.StandardPMapGetSetter[pcommon.Map]{
		Getter: func(_ context.Context, m pcommon.Map) (pcommon.Map, error) { return m, nil },
		Setter: func(_ context.Context, m pcommon.Map, value any) error {
			updated, ok := value.(pcommon.Map)
			if !ok {
				return errors.New("expected a map")
			}
			updated.CopyTo(m)
			return nil
		},
	}
}

func keyReplacementExpression(tb testing.TB, pattern, replacement string, literal bool) ottl.ExprFunc[pcommon.Map] {
	tb.Helper()
	expression, err := replaceAllPatterns(
		keyReplacementTarget(), modeKey,
		keyReplacementGetter(tb, pattern, literal), keyReplacementGetter(tb, replacement, literal),
		ottl.Optional[ottl.FunctionGetter[pcommon.Map]]{}, ottl.Optional[ottl.StringGetter[pcommon.Map]]{},
	)
	require.NoError(tb, err)
	return expression
}

func requireReplacementMapsEqual(t *testing.T, expected, actual pcommon.Map) {
	t.Helper()
	require.Equal(t, expected.AsRaw(), actual.AsRaw())
	keys := func(m pcommon.Map) []string {
		result := make([]string, 0, m.Len())
		for key := range m.All() {
			result = append(result, key)
		}
		return result
	}
	require.Equal(t, keys(expected), keys(actual))
}

func TestReplaceAllPatternsConstantKeyEquivalence(t *testing.T) {
	keys := []string{"old.field", "new.field", "field_0", "field_1", "common", "\u00e9", "bad\xff_key"}
	for _, tt := range []struct {
		pattern     string
		replacement string
	}{
		{`old\.field`, "new.field"},
		{`field_`, "attribute_"},
		{`.*`, "common"},
		{`^(old)(.*)$`, `${2}_${1}`},
		{`^field`, "new"},
		{`field$`, "new"},
		{`(?i)FIELD`, "new"},
		{`\x{FFFD}`, "new"},
		{"\u00e9", "e"},
		{``, "-"},
		{`(field)`, `${1}$$`},
		{`field`, `$missing`},
		{`missing`, "unused"},
	} {
		t.Run(tt.pattern+"/"+tt.replacement, func(t *testing.T) {
			constant := keyReplacementExpression(t, tt.pattern, tt.replacement, true)
			dynamic := keyReplacementExpression(t, tt.pattern, tt.replacement, false)
			random := rand.New(rand.NewPCG(1234, 5678))
			for range 50 {
				input := pcommon.NewMap()
				for range random.IntN(32) {
					key := keys[random.IntN(len(keys))]
					switch random.IntN(5) {
					case 0:
						input.PutStr(key, "value")
					case 1:
						input.PutInt(key, 42)
					case 2:
						input.PutEmptyBytes(key).FromRaw([]byte{0, 1, 255})
					case 3:
						input.PutEmptyMap(key).PutEmptySlice("nested").AppendEmpty().SetBool(true)
					case 4:
						input.PutEmpty(key)
					}
				}
				actual, expected := pcommon.NewMap(), pcommon.NewMap()
				input.CopyTo(actual)
				input.CopyTo(expected)
				_, err := constant(t.Context(), actual)
				require.NoError(t, err)
				_, err = dynamic(t.Context(), expected)
				require.NoError(t, err)
				requireReplacementMapsEqual(t, expected, actual)
			}
		})
	}
}

func TestReplaceAllPatternsSequentialCollisionOrder(t *testing.T) {
	input := pcommon.NewMap()
	input.PutStr("old", "first")
	input.PutStr("middle", "second")
	input.PutInt("tail", 3)
	expected := pcommon.NewMap()
	input.CopyTo(expected)
	for _, tt := range []struct{ pattern, replacement string }{
		{"old", "new"},
		{"new|middle", "common"},
	} {
		_, err := keyReplacementExpression(t, tt.pattern, tt.replacement, true)(t.Context(), input)
		require.NoError(t, err)
		_, err = keyReplacementExpression(t, tt.pattern, tt.replacement, false)(t.Context(), expected)
		require.NoError(t, err)
		requireReplacementMapsEqual(t, expected, input)
	}
	value, ok := input.Get("common")
	require.True(t, ok)
	require.Equal(t, "second", value.Str())
}

func TestReplaceAllPatternsConcurrentConstantKeys(t *testing.T) {
	expression := keyReplacementExpression(t, `old\.field`, "new.field", true)
	for i := range 16 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			for range 50 {
				m := pcommon.NewMap()
				m.PutStr("old.field", "value")
				_, err := expression(t.Context(), m)
				require.NoError(t, err)
				value, exists := m.Get("new.field")
				require.True(t, exists)
				require.Equal(t, "value", value.Str())
			}
		})
	}
}

func BenchmarkReplaceAllPatternsKeys(b *testing.B) {
	for _, size := range []int{8, 32, 128} {
		for _, scenario := range []string{"match", "no_match", "collision"} {
			b.Run(fmt.Sprintf("%d/%s", size, scenario), func(b *testing.B) {
				input := pcommon.NewMap()
				for i := range size {
					input.PutInt(fmt.Sprintf("field_%d", i), int64(i))
				}
				if scenario != "no_match" {
					input.PutStr("old.field", "value")
				}
				if scenario == "collision" {
					input.PutStr("new.field", "existing")
				}
				expression := keyReplacementExpression(b, `old\.field`, "new.field", true)
				b.ReportAllocs()
				for b.Loop() {
					m := pcommon.NewMap()
					input.CopyTo(m)
					if _, err := expression(b.Context(), m); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
