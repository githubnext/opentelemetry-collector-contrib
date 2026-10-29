// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ottlfuncs // import "github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/ottlfuncs"

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"strings"

	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl"
)

const (
	modeKey   = "key"
	modeValue = "value"
)

type replaceAllPatternsArguments[K any] struct {
	Target            ottl.PMapGetSetter[K]
	Mode              string
	RegexPattern      ottl.StringGetter[K]
	Replacement       ottl.StringGetter[K]
	Function          ottl.Optional[ottl.FunctionGetter[K]]
	ReplacementFormat ottl.Optional[ottl.StringGetter[K]]
}

// NewReplaceAllPatternsFactory returns a factory for the replace_all_patterns OTTL function.
// See https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/pkg/ottl/ottlfuncs/README.md#replace_all_patterns
func NewReplaceAllPatternsFactory[K any]() ottl.Factory[K] {
	return ottl.NewFactory("replace_all_patterns", &replaceAllPatternsArguments[K]{}, createReplaceAllPatternsFunction[K])
}

func createReplaceAllPatternsFunction[K any](_ ottl.FunctionContext, oArgs ottl.Arguments) (ottl.ExprFunc[K], error) {
	args, ok := oArgs.(*replaceAllPatternsArguments[K])

	if !ok {
		return nil, errors.New("ReplaceAllPatternsFactory args must be of type *replaceAllPatternsArguments[K]")
	}

	return replaceAllPatterns(args.Target, args.Mode, args.RegexPattern, args.Replacement, args.Function, args.ReplacementFormat)
}

func replaceAllPatterns[K any](target ottl.PMapGetSetter[K], mode string, regexPattern, replacement ottl.StringGetter[K], fn ottl.Optional[ottl.FunctionGetter[K]], replacementFormat ottl.Optional[ottl.StringGetter[K]]) (ottl.ExprFunc[K], error) {
	compiledPattern, err := newDynamicRegex("replace_all_patterns", regexPattern)
	if err != nil {
		return nil, err
	}
	if mode != modeValue && mode != modeKey {
		return nil, fmt.Errorf("invalid mode %v, must be either 'key' or 'value'", mode)
	}

	var rewriteKeys func(pcommon.Map) (bool, error)
	if replacementValue, literal := ottl.GetLiteralValue(replacement); mode == modeKey && compiledPattern.value != nil && literal && fn.IsEmpty() {
		rewriteKeys, err = newKeyPatternRewriter(compiledPattern.value, replacementValue)
		if err != nil {
			return nil, err
		}
	}

	return func(ctx context.Context, tCtx K) (any, error) {
		val, err := target.Get(ctx, tCtx)
		if err != nil {
			return nil, err
		}

		var replacementVal string
		replacementVal, err = replacement.Get(ctx, tCtx)
		if err != nil {
			return nil, err
		}

		cp, err := compiledPattern.compile(ctx, tCtx)
		if err != nil {
			return nil, err
		}

		switch mode {
		case modeValue:
			for _, value := range val.All() {
				if value.Type() != pcommon.ValueTypeStr || !cp.MatchString(value.Str()) {
					continue
				}
				if !fn.IsEmpty() {
					updatedString, err := applyOptReplaceFunction(ctx, tCtx, cp, fn, value.Str(), replacementVal, replacementFormat)
					if err != nil {
						continue
					}
					value.SetStr(updatedString)
				} else {
					value.SetStr(cp.ReplaceAllString(value.Str(), replacementVal))
				}
			}
		case modeKey:
			if rewriteKeys != nil {
				rewritten, err := rewriteKeys(val)
				if err != nil {
					return nil, err
				}
				if rewritten {
					break
				}
			}
			hasMatch := false
			for key := range val.All() {
				if cp.MatchString(key) {
					hasMatch = true
					break
				}
			}
			if !hasMatch {
				break
			}
			// Rebuilding retains collision precedence and optional-function behavior
			// when an order-preserving in-place rewrite is unavailable.
			updated := pcommon.NewMap()
			updated.EnsureCapacity(val.Len())
			for key, value := range val.All() {
				if !cp.MatchString(key) {
					value.MoveTo(updated.PutEmpty(key))
					continue
				}
				if !fn.IsEmpty() {
					updatedKey, err := applyOptReplaceFunction(ctx, tCtx, cp, fn, key, replacementVal, replacementFormat)
					if err != nil {
						continue
					}
					value.MoveTo(updated.PutEmpty(updatedKey))
				} else {
					value.MoveTo(updated.PutEmpty(cp.ReplaceAllString(key, replacementVal)))
				}
			}
			updated.MoveTo(val)
		}
		return nil, target.Set(ctx, tCtx, val)
	}, nil
}

func newKeyPatternRewriter(pattern *regexp.Regexp, replacement string) (func(pcommon.Map) (bool, error), error) {
	matchKey := pattern.MatchString
	replaceKey := func(key string) string { return pattern.ReplaceAllString(key, replacement) }
	parsed, err := syntax.Parse(pattern.String(), syntax.Perl)
	if err != nil {
		return nil, err
	}
	asciiLiteral := parsed.Op == syntax.OpLiteral && parsed.Flags&syntax.FoldCase == 0
	for _, r := range parsed.Rune {
		if r > 127 {
			asciiLiteral = false
			break
		}
	}
	if asciiLiteral {
		literal := string(parsed.Rune)
		matchKey = func(key string) bool { return strings.Contains(key, literal) }
		if !strings.Contains(replacement, "$") {
			replaceKey = func(key string) string { return strings.ReplaceAll(key, literal, replacement) }
		}
	}

	return func(attributes pcommon.Map) (bool, error) {
		type rename struct{ from, to string }
		var changes []rename
		for key := range attributes.All() {
			if !matchKey(key) {
				continue
			}
			updated := replaceKey(key)
			if updated == key {
				continue
			}
			if _, exists := attributes.Get(updated); exists {
				return false, nil
			}
			for _, change := range changes {
				if change.to == updated {
					return false, nil
				}
			}
			changes = append(changes, rename{from: key, to: updated})
		}
		// Changing entry order can change which value wins a later collision.
		for _, change := range changes {
			if !attributes.RenameKey(change.from, change.to) {
				return false, fmt.Errorf("cannot rename validated map key %q to %q", change.from, change.to)
			}
		}
		return true, nil
	}, nil
}
