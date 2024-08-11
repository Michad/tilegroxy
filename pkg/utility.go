// Copyright 2024 Michael Davis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package pkg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"

	"github.com/Michad/tilegroxy/pkg/static"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var packageName = static.GetPackage()
var version, ref, buildDate = static.GetVersionInformation()
var tracer trace.Tracer = otel.Tracer(packageName)

type Image = []byte

func ParseZoomString(str string) ([]int, error) {
	const errorMessage = "could not parse zoom %v"

	commaSplit := strings.Split(str, ",")

	var result []int

	for _, entry := range commaSplit {
		dashSplit := strings.Split(entry, "-")

		switch len(dashSplit) {
		case 1:
			singleZoom, err := strconv.Atoi(dashSplit[0])

			if singleZoom < 0 || singleZoom > MaxZoom {
				return nil, errors.New("zoom out of range")
			}

			if err == nil {
				result = append(result, singleZoom)
			} else {
				return nil, fmt.Errorf(errorMessage, entry)
			}
		case 2:
			start, err := strconv.Atoi(dashSplit[0])
			end, err2 := strconv.Atoi(dashSplit[1])
			if err != nil || err2 != nil {
				return nil, errors.Join(err, err2)
			}

			if end < start {
				return nil, errors.New("zoom range must start before it ends")
			}

			if start < 0 || end > MaxZoom {
				return nil, errors.New("zoom out of range")
			}

			for i := start; i <= end; i++ {
				result = append(result, i)
			}
		default:
			return nil, fmt.Errorf(errorMessage, entry)
		}
	}

	return result, nil
}

// Find any string values that start with `keyTag.keyName` and replace it with replacer(keyName). Replaces the full value. Used for avoiding secrets in config so your configuration can be placed in source control
func ReplaceConfigValues(rawConfig map[string]interface{}, keyTag string, replacer func(string) (string, error)) (map[string]interface{}, error) {
	var err error
	result := make(map[string]interface{})
	for k, v := range rawConfig {
		if vMap, ok := v.(map[string]interface{}); ok {
			result[k], err = ReplaceConfigValues(vMap, keyTag, replacer)
		} else if vStr, ok := v.(string); ok {
			if strings.Index(vStr, keyTag+".") == 0 {
				varName := vStr[len(keyTag)+1:]
				slog.Debug("Replacing " + keyTag + " var " + varName)

				result[k], err = replacer(varName)
				if err != nil {
					break
				}
			} else {
				result[k] = vStr
			}
		} else {
			result[k] = v
		}
	}

	return result, err
}

// Find any string values that start with `env.` and interpret the rest as an environment variable. Replaces the full value with the contents of the respective environment variable. Useful for avoiding secrets in config so your configuration can be placed in source control
func ReplaceEnv(rawConfig map[string]interface{}) map[string]interface{} {
	result, _ := ReplaceConfigValues(rawConfig, "env", func(s string) (string, error) { return os.Getenv(s), nil })

	return result
}

func Ternary[T any](cond bool, a T, b T) T {
	if cond {
		return a
	}
	return b
}

func RandomString() string {
	i := rand.Int64()
	i2 := rand.Int64()
	return strconv.FormatInt(i, 36) + strconv.FormatInt(i2, 36)
}

// Handles making a new context and span for use in a provider that calls another provider. Make sure to End the span that is returned
func MakeChildSpan(ctx context.Context, newRequest *TileRequest, providerName string, childSpanName string, functionName string) (context.Context, trace.Span) {
	spanName := providerName

	if childSpanName != "" {
		spanName += "-" + childSpanName
	}

	newCtx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))

	if span.IsRecording() {
		span.SetAttributes(
			attribute.String("service.name", "tilegroxy"),
			attribute.String("service.version", version+"-"+ref),
			attribute.String("service.build", buildDate),
			attribute.String("code.function", functionName),
		)

		if newRequest != nil {
			span.SetAttributes(
				attribute.String("tilegroxy.layer.name", newRequest.LayerName),
				attribute.Int("tilegroxy.coordinate.x", newRequest.X),
				attribute.Int("tilegroxy.coordinate.y", newRequest.Y),
				attribute.Int("tilegroxy.coordinate.z", newRequest.Z),
			)
		}
	}

	return newCtx, span
}
