/*
 Copyright 2023-KylinSoft Co.,Ltd.

 kse-rescheduler is about rescheduling terminated or crashloopbackoff pods according to the scheduling-retries defined
 in annotations. some pods scheduled to a specific node, but can't run normally, so we try to reschedule the pods some times according to
 the scheduling-retries defined in annotations.
*/


package version

import (
	"fmt"
	"runtime"
)

var (
	AppVersion      = "1.0"
	VersionSuffix   = ""
	GitCommit       = ""
)

func Version() string {
	version := AppVersion

	if VersionSuffix != "" {
		version += VersionSuffix
	}

	return version
}

func VersionWithMetadata() string {
	version := Version()
	if VersionSuffix != "" && GitCommit != "" {
		version += "+" + truncate(GitCommit, 14)
	}

	return version
}

func truncate(s string, maxLen int) string {
	if len(s) < maxLen {
		return s
	}
	return s[:maxLen]
}

func DisplayVersion() string {
	return fmt.Sprintf("kse-rescheduler v%s %s %s/%s", VersionWithMetadata(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
}