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

func GetPackage() string {
	return "github.com/michad/tilegroxy"
}

// Returns the version (vX.Y.Z, with placeholders for unofficial builds), the git ref, and the build timestamp
func GetVersionInformation() (string, string, string) {
	myVersion := tilegroxyVersion

	if myVersion == "" {
		myVersion = "v" + strconv.Itoa(majorVersion) + ".X.Y" // Default if building locally
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
