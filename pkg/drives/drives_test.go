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
		t.Logf("Drive: %s (%s) [%s] wsl=%v padrao=%v - %d MB Free / %d MB Total",
			d.Letter, d.VolumeLabel, d.DriveType, d.IsWSL, d.DefaultSelected, d.FreeBytes/(1024*1024), d.TotalBytes/(1024*1024))
	}
	if len(drivesList) == 0 {
		t.Errorf("Expected at least one drive, found 0")
	}
	for _, d := range drivesList {
		if d.IsWSL && d.DefaultSelected {
			t.Errorf("volume WSL %s não pode vir marcado por padrão", d.Letter)
		}
	}
}

func TestIsWSLVolume(t *testing.T) {
	cases := []struct {
		fileSystem string
		path       string
		want       bool
	}{
		{"9P", `Z:\`, true},
		{"9p", `Z:\`, true},
		{"NTFS", `\\wsl$\Ubuntu\`, true},
		{"NTFS", `\\wsl.localhost\Ubuntu\`, true},
		{"NTFS", `\\WSL$\Debian\`, true},
		{"NTFS", `C:\`, false},
		{"exFAT", `E:\`, false},
		{"", `D:\`, false},
		{"NTFS", `\\servidor\compartilhamento\`, false},
		{"NTFS", `C:\wsl\algo`, false},
	}

	for _, tc := range cases {
		if got := IsWSLVolume(tc.fileSystem, tc.path); got != tc.want {
			t.Errorf("IsWSLVolume(%q, %q) = %v, esperado %v", tc.fileSystem, tc.path, got, tc.want)
		}
	}
}

func TestIsDefaultSelected(t *testing.T) {
	cases := []struct {
		driveType  string
		fileSystem string
		path       string
		want       bool
	}{
		{DriveTypeFixed, "NTFS", `C:\`, true},
		{DriveTypeRemovable, "exFAT", `E:\`, true},
		{DriveTypeRAMDisk, "NTFS", `R:\`, true},
		{DriveTypeNetwork, "NTFS", `Y:\`, false},
		{DriveTypeCDRom, "CDFS", `D:\`, false},
		{DriveTypeUnavailable, "N/A", `X:\`, false},
		{DriveTypeFixed, "9P", `Z:\`, false},
		{DriveTypeNetwork, "NTFS", `\\wsl$\Ubuntu\`, false},
	}

	for _, tc := range cases {
		if got := IsDefaultSelected(tc.driveType, tc.fileSystem, tc.path); got != tc.want {
			t.Errorf("IsDefaultSelected(%q, %q, %q) = %v, esperado %v", tc.driveType, tc.fileSystem, tc.path, got, tc.want)
		}
	}
}
