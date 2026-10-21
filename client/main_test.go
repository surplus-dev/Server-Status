package main

import "testing"

func TestDiskMountpointKey(t *testing.T) {
	tests := []struct {
		goos       string
		mountpoint string
		want       string
	}{
		{"windows", "C:", "c:"},
		{"windows", `C:\`, "c:"},
		{"windows", `c:\`, "c:"},
		{"windows", "C:/", "c:"},
		{"windows", `D:\`, "d:"},
		{"windows", `C:\Data\`, `c:\data`},
		{"windows", "c:/data", `c:\data`},
		{"windows", `C:\Other`, `c:\other`},
		{"windows", `\\SERVER\Share\`, `\\server\share`},
		{"linux", "/", "/"},
		{"linux", "/data", "/data"},
		{"linux", "/Data", "/Data"},
		{"linux", `/data\name`, `/data\name`},
		{"linux", "", ""},
		{"windows", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.goos+"/"+tt.mountpoint, func(t *testing.T) {
			if got := diskMountpointKey(tt.mountpoint, tt.goos); got != tt.want {
				t.Errorf("diskMountpointKey(%q, %q) = %q; want %q", tt.mountpoint, tt.goos, got, tt.want)
			}
		})
	}
}
