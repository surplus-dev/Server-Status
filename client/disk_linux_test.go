package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectDisksIncludesNetworkMounts(t *testing.T) {
	tests := []struct {
		fstype string
		device string
	}{
		{"nfs", "192.0.2.10:/export/data"},
		{"nfs4", "192.0.2.10:/export/data"},
		{"cifs", "//192.0.2.10/share"},
	}

	for _, tt := range tests {
		t.Run(tt.fstype, func(t *testing.T) {
			dir := t.TempDir()
			mountpoint := filepath.Join(dir, "network-data")
			if err := os.Mkdir(mountpoint, 0755); err != nil {
				t.Fatal(err)
			}
			mountinfo := filepath.Join(dir, "mountinfo")
			line := fmt.Sprintf("100 1 0:50 / %s rw - %s %s rw\n", mountpoint, tt.fstype, tt.device)
			if err := os.WriteFile(mountinfo, []byte(line), 0644); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOST_PROC_MOUNTINFO", mountinfo)

			disks, err := collectDisks()
			if err != nil {
				t.Fatal(err)
			}
			for _, metric := range disks {
				if metric["mountpoint"] != mountpoint {
					continue
				}
				if metric["device"] != tt.device {
					t.Errorf("device = %v; want %s", metric["device"], tt.device)
				}
				if total, ok := metric["total_bytes"].(uint64); !ok || total == 0 {
					t.Errorf("missing capacity: %+v", metric)
				}
				if _, ok := metric["free_bytes"].(uint64); !ok {
					t.Errorf("missing free space: %+v", metric)
				}
				return
			}
			t.Fatalf("network mount %s was not collected: %+v", mountpoint, disks)
		})
	}
}
