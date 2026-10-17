// Copyright 2026 Michael Davis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package placeholder

import (
	"net/url"
	"strconv"
	"strings"
)

// One substituted placeholder. Missing is set when it couldn't be resolved and has no default
type Value struct {
	Text    string
	Source  string
	Missing bool
}

// Operator input and tile coordinates are trusted. Everything else comes from the request
func (v Value) Trusted() bool {
	return v.Source == SourceEnv || v.Source == SourceSecret || v.Source == SourceTile
}

// Receives a rendered Template piece by piece and decides how each value is inserted
type Sink interface {
	Literal(text string)
	Value(v Value)
}

// Inserts every value as-is
type RawSink struct {
	b strings.Builder
}

func (s *RawSink) Literal(text string) {
	s.b.WriteString(text)
}

func (s *RawSink) Value(v Value) {
	s.b.WriteString(v.Text)
}

func (s *RawSink) String() string {
	return s.b.String()
}

// Escapes untrusted values so a "?", "#" or "/" can't alter the URL. Trusted values may supply whole URL parts
type URLSink struct {
	b strings.Builder
	// Tracked from literal text only, so a "?" inside a value can't change how later values are escaped
	inQuery bool
}

func (s *URLSink) Literal(text string) {
	s.b.WriteString(text)

	if strings.Contains(text, "?") {
		s.inQuery = true
	}
}

func (s *URLSink) Value(v Value) {
	switch {
	case v.Trusted():
		s.b.WriteString(v.Text)
	case s.inQuery:
		s.b.WriteString(url.QueryEscape(v.Text))
	default:
		s.b.WriteString(url.PathEscape(v.Text))
	}
}

func (s *URLSink) String() string {
	return s.b.String()
}

// Inlines tile coordinates, which are numbers, and passes every other value as a $N prepared statement parameter
type SQLSink struct {
	b      strings.Builder
	params []any
	next   int
}

// firstParam is the $N number of the first parameter this sink adds
func NewSQLSink(firstParam int) *SQLSink {
	return &SQLSink{next: firstParam}
}

func (s *SQLSink) Literal(text string) {
	s.b.WriteString(text)
}

func (s *SQLSink) Value(v Value) {
	if v.Source == SourceTile {
		s.b.WriteString(v.Text)
		return
	}

	if v.Missing {
		s.params = append(s.params, nil)
	} else {
		s.params = append(s.params, v.Text)
	}

	s.b.WriteString("$" + strconv.Itoa(s.next))
	s.next++
}

func (s *SQLSink) String() string {
	return s.b.String()
}

func (s *SQLSink) Params() []any {
	return s.params
}
