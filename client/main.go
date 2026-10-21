package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

var (
	statusURL = "https://status.mu.io.kr/api/metrics"
	serverID  = "gpu-server-1"
	apiToken  = "change-this-token"
)

var client = &http.Client{
	Timeout: 10 * time.Second,
}

func initJsonOpen() {
	jsonFile := "set.json"

	jsonValue, err := os.ReadFile(jsonFile)
	if err != nil {
		fmt.Println(err)
	}

	config := map[string]string{}

	err = json.Unmarshal(jsonValue, &config)
	if err != nil {
		fmt.Println(err)
	}

	statusURL = config["statusURL"]
	serverID = config["serverID"]
	apiToken = config["token"]
}

func main() {
	initJsonOpen()

	for {
		if err := sendMetrics(); err != nil {
			fmt.Println("전송 실패:", err)
		} else {
			fmt.Println("전송 완료:", time.Now().Format(time.RFC3339))
		}

		time.Sleep(3 * time.Minute)
	}
}

func sendMetrics() error {
	metrics, err := collectMetrics()
	if err != nil {
		return err
	}

	body, err := json.Marshal(metrics)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(
		http.MethodPost,
		statusURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("서버 응답 코드: %d", resp.StatusCode)
	}

	return nil
}

func collectMetrics() (map[string]any, error) {
	cpuUsage, err := cpu.Percent(time.Second, false)
	if err != nil {
		return nil, err
	}

	memory, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
	}

	hostname, _ := os.Hostname()

	metrics := map[string]any{
		"token":           apiToken,
		"server_id":       serverID,
		"hostname":        hostname,
		"timestamp":       time.Now().UTC().Format(time.RFC3339),
		"cpu_percent":     cpuUsage[0],
		"memory_percent":  memory.UsedPercent,
		"memory_used_mb":  memory.Used / 1024 / 1024,
		"memory_total_mb": memory.Total / 1024 / 1024,
	}

	disks, err := collectDisks()
	if err != nil {
		metrics["disk_error"] = err.Error()
		metrics["disks"] = []map[string]any{}
	} else {
		metrics["disks"] = disks
	}

	gpus, err := collectGPUs()
	if err != nil {
		metrics["gpu_error"] = err.Error()
		metrics["gpus"] = []map[string]any{}
	} else {
		metrics["gpus"] = gpus
	}

	return metrics, nil
}

func collectDisks() ([]map[string]any, error) {
	// Include network and container mounts; the server filters display-only entries.
	partitions, err := disk.Partitions(true)
	if err != nil {
		return nil, err
	}

	// Containers may omit the root filesystem from physical partitions.
	rootPath, err := filepath.Abs(string(os.PathSeparator))
	if err != nil {
		return nil, err
	}

	rootKey := diskMountpointKey(rootPath, runtime.GOOS)
	hasRoot := false
	for _, partition := range partitions {
		if diskMountpointKey(partition.Mountpoint, runtime.GOOS) == rootKey {
			hasRoot = true
			break
		}
	}
	if !hasRoot {
		partitions = append(partitions, disk.PartitionStat{Mountpoint: rootPath})
	}

	sort.Slice(partitions, func(i, j int) bool {
		return partitions[i].Mountpoint < partitions[j].Mountpoint
	})

	disks := make([]map[string]any, 0, len(partitions))
	seen := make(map[string]bool)
	for _, partition := range partitions {
		mountpointKey := diskMountpointKey(partition.Mountpoint, runtime.GOOS)
		if partition.Mountpoint == "" || seen[mountpointKey] {
			continue
		}
		seen[mountpointKey] = true

		metric := map[string]any{
			"mountpoint": partition.Mountpoint,
			"device":     partition.Device,
			"fstype":     partition.Fstype,
		}

		usage, err := disk.Usage(partition.Mountpoint)
		if err != nil {
			metric["error"] = err.Error()
		} else {
			if usage.Total == 0 {
				continue
			}
			metric["fstype"] = usage.Fstype
			metric["total_bytes"] = usage.Total
			metric["used_bytes"] = usage.Used
			metric["free_bytes"] = usage.Free
			metric["used_percent"] = usage.UsedPercent
		}

		disks = append(disks, metric)
	}

	return disks, nil
}

func diskMountpointKey(mountpoint, goos string) string {
	if goos == "windows" {
		// Windows reports drive roots as both "C:" and `C:\`.
		mountpoint = strings.ReplaceAll(mountpoint, "/", `\`)
		mountpoint = strings.TrimRight(mountpoint, `\`)
		return strings.ToLower(mountpoint)
	}

	return mountpoint
}

func collectGPUs() ([]map[string]any, error) {
	output, err := exec.Command(
		"nvidia-smi",
		"--query-gpu=index,utilization.gpu,memory.used,memory.total,temperature.gpu",
		"--format=csv,noheader,nounits",
	).Output()
	if err != nil {
		return nil, err
	}

	var gpus []map[string]any

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	for _, line := range lines {
		fields := strings.Split(line, ",")
		if len(fields) != 5 {
			continue
		}

		index, _ := strconv.Atoi(strings.TrimSpace(fields[0]))
		utilization, _ := strconv.ParseFloat(strings.TrimSpace(fields[1]), 64)
		memoryUsed, _ := strconv.ParseFloat(strings.TrimSpace(fields[2]), 64)
		memoryTotal, _ := strconv.ParseFloat(strings.TrimSpace(fields[3]), 64)
		temperature, _ := strconv.ParseFloat(strings.TrimSpace(fields[4]), 64)

		gpus = append(gpus, map[string]any{
			"index":           index,
			"util_percent":    utilization,
			"memory_used_mb":  memoryUsed,
			"memory_total_mb": memoryTotal,
			"temperature_c":   temperature,
		})
	}

	return gpus, nil
}
