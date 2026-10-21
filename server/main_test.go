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
