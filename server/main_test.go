package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMakeDiskStatusHTMLFiltersDisks(t *testing.T) {
	tests := []struct {
		mountpoint string
		fstype     string
		want       bool
	}{
		{"/", "ext4", true},
		{"/", "overlay", true},
		{"/", "tmpfs", true},
		{"/", "squashfs", true},
		{"/data", "ext4", true},
		{"/mnt/data", "nfs", true},
		{"/mnt/data", "nfs4", true},
		{"/mnt/share", "cifs", true},
		{"/mnt/usb", "vfat", true},
		{"/boot-data", "ext4", true},
		{"/tmp-data", "ext4", true},
		{"/var/tmp-data", "ext4", true},
		{"/runway", "ext4", true},
		{"/data/boot", "ext4", true},
		{"/boot", "ext4", false},
		{"/boot/", "ext4", false},
		{"/boot/efi", "vfat", false},
		{"/boot/efi", "", false},
		{"/boot/efi/EFI", "vfat", false},
		{"/efi", "vfat", false},
		{"/tmp", "ext4", false},
		{"/tmp/cache", "ext4", false},
		{"/var/tmp", "ext4", false},
		{"/run/user/1000", "ext4", false},
		{"/var/run", "ext4", false},
		{"/dev", "ext4", false},
		{"/proc", "ext4", false},
		{"/sys", "ext4", false},
		{"/mnt/ram", "tmpfs", false},
		{"/mnt/devices", "devtmpfs", false},
		{"/mnt/ram", "ramfs", false},
		{"/mnt/ram", "TMPFS", false},
		{"/snap/core/1", "squashfs", false},
		{"/mnt/proc", "procfs", false},
		{"/mnt/sys", "sysfs", false},
		{"/mnt/devices", "devfs", false},
		{"/mnt/pts", "devpts", false},
		{"/mnt/cgroup", "cgroup", false},
		{"/mnt/cgroup", "cgroup2", false},
		{"", "", false},
		{"C:", "NTFS", true},
		{`c:\`, "NTFS", true},
		{"D:", "NTFS", true},
		{"E:", "FAT32", true},
	}

	for _, tt := range tests {
		t.Run(tt.mountpoint+"/"+tt.fstype, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				"disks": []map[string]any{{
					"mountpoint":  tt.mountpoint,
					"fstype":      tt.fstype,
					"total_bytes": 1024,
					"free_bytes":  512,
				}},
			})
			if err != nil {
				t.Fatal(err)
			}

			got := makeDiskStatusHTML(body)
			if visible := strings.Contains(got, "<h3>DISK "); visible != tt.want {
				t.Errorf("disk visibility = %t; want %t; HTML: %s", visible, tt.want, got)
			}
			if !tt.want && got != "<p>DISK: N/A</p>" {
				t.Errorf("all disks filtered: got %q; want N/A", got)
			}
		})
	}
}

func TestMakeDiskStatusHTMLDeduplicatesDisks(t *testing.T) {
	metric := func(mountpoint, device string) map[string]any {
		return map[string]any{
			"mountpoint":   mountpoint,
			"device":       device,
			"total_bytes":  1024 * 1024 * 1024,
			"used_bytes":   512 * 1024 * 1024,
			"free_bytes":   512 * 1024 * 1024,
			"used_percent": 50,
		}
	}
	failed := metric("C:", "C:")
	failed["error"] = "permission denied"
	full := metric(`c:\`, "C:")
	full["free_bytes"] = 0
	full["used_bytes"] = full["total_bytes"]
	full["used_percent"] = 100

	tests := []struct {
		name   string
		disks  []map[string]any
		count  int
		want   []string
		absent []string
	}{
		{
			name: "Windows drive aliases",
			disks: []map[string]any{
				metric("C:", "C:"), metric(`C:\`, "C:"), metric("c:/", ""), metric("D:", "D:"),
			},
			count:  2,
			want:   []string{"<h3>DISK C:</h3>", "<h3>DISK D:</h3>"},
			absent: []string{"C: (C:)"},
		},
		{
			name:   "prefer available usage including zero free space",
			disks:  []map[string]any{failed, full, metric("C:/", "")},
			count:  1,
			want:   []string{`<h3>DISK c:\</h3>`, "남은 용량: 0.0 B", "사용 용량: 1.0 GiB (100.0%)"},
			absent: []string{"N/A", "(C:)"},
		},
		{
			name: "Windows mounted folders",
			disks: []map[string]any{
				metric(`C:\Data`, ""), metric("c:/data/", ""), metric(`C:\Other`, ""),
			},
			count: 2,
			want:  []string{`<h3>DISK C:\Data</h3>`, `<h3>DISK C:\Other</h3>`},
		},
		{
			name: "POSIX paths retain case and backslashes",
			disks: []map[string]any{
				metric("/data", ""), metric("/Data", ""), metric(`/data\name`, ""), metric("/data/name", ""),
			},
			count: 4,
			want:  []string{"<h3>DISK /data</h3>", "<h3>DISK /Data</h3>", `<h3>DISK /data\name</h3>`, "<h3>DISK /data/name</h3>"},
		},
		{
			name: "Windows network share aliases",
			disks: []map[string]any{
				metric(`\\SERVER\Share`, ""), metric(`\\server\share\`, ""),
			},
			count: 1,
			want:  []string{`<h3>DISK \\SERVER\Share</h3>`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"disks": tt.disks})
			if err != nil {
				t.Fatal(err)
			}
			got := makeDiskStatusHTML(body)
			if count := strings.Count(got, "<h3>DISK "); count != tt.count {
				t.Errorf("disk count = %d; want %d; HTML: %s", count, tt.count, got)
			}
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in HTML: %s", want, got)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(got, absent) {
					t.Errorf("unexpected %q in HTML: %s", absent, got)
				}
			}
		})
	}
}

func TestMakeDiskStatusHTMLHidesNetworkAddresses(t *testing.T) {
	tests := []struct {
		device     string
		fstype     string
		wantDevice bool
	}{
		{"192.0.2.10:/export/data", "nfs", false},
		{"storage.example:/export/data", "nfs4", false},
		{"[2001:db8::1]:/export/data", "nfs4", false},
		{"192.0.2.10:/export/data", "", false},
		{"[::1]:/export/data", "", false},
		{"//192.0.2.10/share", "cifs", false},
		{`\\192.0.2.10\share`, "", false},
		{"user@storage.example:/data", "fuse.sshfs", false},
		{"/export/data", "nfs", false},
		{"/dev/nvme0n1p2", "ext4", true},
	}

	for _, tt := range tests {
		t.Run(tt.fstype+"/"+tt.device, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				"disks": []map[string]any{{
					"mountpoint":  "/mnt/data",
					"device":      tt.device,
					"fstype":      tt.fstype,
					"total_bytes": 1024,
					"free_bytes":  512,
				}},
			})
			if err != nil {
				t.Fatal(err)
			}

			got := makeDiskStatusHTML(body)
			if strings.Count(got, "<h3>DISK ") != 1 || !strings.Contains(got, "/mnt/data") {
				t.Fatalf("mountpoint not displayed: %s", got)
			}
			if shown := strings.Contains(got, tt.device); shown != tt.wantDevice {
				t.Errorf("device visibility = %t; want %t; HTML: %s", shown, tt.wantDevice, got)
			}
			if !tt.wantDevice && !strings.Contains(got, "<h3>DISK /mnt/data</h3>") {
				t.Errorf("network disk must show only its mountpoint: %s", got)
			}
		})
	}
}
