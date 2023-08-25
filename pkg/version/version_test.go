/*
 Copyright 2023-KylinSoft Co.,Ltd.

 kse-rescheduler is about rescheduling terminated or crashloopbackoff pods according to the scheduling-retries defined
 in annotations. some pods scheduled to a specific node, but can't run normally, so we try to reschedule the pods some times according to
 the scheduling-retries defined in annotations.
*/


package version

import "testing"

func TestVersionWithMetadata(t *testing.T) {
	tests := []struct {
		name          string
		appVersion    string
		versionSuffix string
		gitCommit     string
		want          string
	}{
		{
			name:          "development version without git commit",
			appVersion:    "0.6.0",
			versionSuffix: "-beta1",
			gitCommit:     "",
			want:          "0.6.0-beta1",
		},
		{
			name:          "development version with short git commit",
			appVersion:    "0.6.0",
			versionSuffix: "-beta1",
			gitCommit:     "d4b6",
			want:          "0.6.0-beta1+d4b6",
		},
		{
			name:          "development version with git commit to truncate",
			appVersion:    "0.6.0",
			versionSuffix: "-beta1",
			gitCommit:     "6e7b4ee4b2ee0b460f0df54fb90b566ecccf7ce2",
			want:          "0.6.0-beta1+6e7b4ee4b2ee0b",
		},
		{
			name:          "release version",
			appVersion:    "1.0.0",
			versionSuffix: "",
			gitCommit:     "6e7b4ee4b2ee0b460f0df54fb90b566ecccf7ce2",
			want:          "1.0.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			AppVersion = tt.appVersion
			VersionSuffix = tt.versionSuffix
			GitCommit = tt.gitCommit

			if got := VersionWithMetadata(); got != tt.want {
				t.Errorf("VersionWithMetadata() = %v, want %v", got, tt.want)
			}
		})
	}
}