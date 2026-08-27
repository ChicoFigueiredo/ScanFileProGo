package drives

import (
	"testing"
)

func TestGetLogicalDrives(t *testing.T) {
	drivesList, err := GetLogicalDrives()
	if err != nil {
		t.Fatalf("GetLogicalDrives error: %v", err)
	}
	t.Logf("Found %d drives:", len(drivesList))
	for _, d := range drivesList {
		t.Logf("Drive: %s (%s) [%s] - %d MB Free / %d MB Total", d.Letter, d.VolumeLabel, d.DriveType, d.FreeBytes/(1024*1024), d.TotalBytes/(1024*1024))
	}
	if len(drivesList) == 0 {
		t.Errorf("Expected at least one drive, found 0")
	}
}
