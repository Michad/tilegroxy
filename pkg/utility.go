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
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
)

type Image = []byte


func ParseZoomString(str string) ([]int, error) {
	const errorMessage = "could not parse zoom %v"

	commaSplit := strings.Split(str, ",")

	var result []int

	for _, entry := range commaSplit {
		dashSplit := strings.Split(entry, "-")

		if len(dashSplit) == 1 {
			singleZoom, err := strconv.Atoi(dashSplit[0])

			if singleZoom < 0 || singleZoom > MaxZoom {
				return nil, errors.New("zoom out of range")
			}

			if err == nil {
				result = append(result, singleZoom)
			} else {
				return nil, fmt.Errorf(errorMessage, entry)
			}
		} else if len(dashSplit) == 2 {
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
		} else {
			return nil, fmt.Errorf(errorMessage, entry)
		}
	}

	return result, nil
}

// Find any string values that start with `env.` and interpret the rest as an environment variable. Replaces the full value with the contents of the respective environment variable. Useful for avoiding secrets in config so your configuration can be placed in source control
func ReplaceEnv(rawConfig map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range rawConfig {
		if vMap, ok := v.(map[string]interface{}); ok {
			result[k] = ReplaceEnv(vMap)
		} else if vStr, ok := v.(string); ok {
			if strings.Index(vStr, "env.") == 0 {
				envVar := vStr[4:]
				slog.Debug("Replacing env var " + envVar)

				result[k] = os.Getenv(envVar)
			} else {
				result[k] = vStr
			}
		} else {
			result[k] = v
		}
	}

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
