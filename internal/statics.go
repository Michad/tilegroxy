package internal

import "strconv"

type Image = []byte

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
