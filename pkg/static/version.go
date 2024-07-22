// Copyright 2024 Michael Davis
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

package static

import "strconv"

const majorVersion = 0

var (
	tilegroxyVersion   string
	tilegroxyBuildRef  string
	tilegroxyBuildDate string
)

// Returns a tuple containing build version information. Returns:
// Version in the format vX.Y.Z - will include placeholders for unofficial builds
// Verson Control System identifier (git ref)
// Timestamp of when it was built
func GetVersionInformation() (string, string, string) {
	myVersion := tilegroxyVersion

	if myVersion == "" {
		myVersion = "v" + strconv.Itoa(majorVersion) + ".X.Y" //Default if building locally
	}

	myRef := tilegroxyBuildRef

	if myRef == "" {
		myRef = "HEAD"
	}

	myDate := tilegroxyBuildDate

	if myDate == "" {
		myDate = "Unknown"
	}

	return myVersion, myRef, myDate
}
