package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
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
    apiToken = config["apiToken"]
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
	req.Header.Set("Authorization", "Bearer "+apiToken)

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
		"server_id":       serverID,
		"hostname":        hostname,
		"timestamp":       time.Now().UTC().Format(time.RFC3339),
		"cpu_percent":     cpuUsage[0],
		"memory_percent":  memory.UsedPercent,
		"memory_used_mb":  memory.Used / 1024 / 1024,
		"memory_total_mb": memory.Total / 1024 / 1024,
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