package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guptarohit/asciigraph"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var (
	token   = "change-this-token"
	dataDir = "./data"
	port    = "8080"
)

var serverIDRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

const (
	historyWindow    = 6 * time.Hour
	historyMaxPoints = 120
)

type historyPoint struct {
	CPU    float64
	RAM    float64
	GPU    float64
	Temp   float64
	HasGPU bool
}

func initJsonOpen() {
	jsonFile := "set.json"

	jsonValue, err := os.ReadFile(jsonFile)
	if err != nil {
		fmt.Println("set.json read error:", err)
		return
	}

	config := map[string]string{}

	err = json.Unmarshal(jsonValue, &config)
	if err != nil {
		fmt.Println("set.json parse error:", err)
		return
	}

	if config["token"] != "" {
		token = config["token"]
	}

	if config["port"] != "" {
		port = config["port"]
	}

	if config["dataDir"] != "" {
		dataDir = config["dataDir"]
	}
}

func main() {
	initJsonOpen()

	r := gin.Default()

	r.GET("/", showStatus)
	r.POST("/api/metrics", saveMetrics)

	fmt.Println("Run in :" + port)

	if err := r.Run(":" + port); err != nil {
		fmt.Println("server error:", err)
	}
}

func normalizeGraphForMobile(s string) string {
	replacer := strings.NewReplacer(
		"╭", "+",
		"╮", "+",
		"╰", "+",
		"╯", "+",
		"─", "-",
		"│", "|",
		"┤", "|",
		"┼", "+",
	)
	return replacer.Replace(s)
}

func saveMetrics(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)

	body, err := io.ReadAll(c.Request.Body)
	if err != nil || !json.Valid(body) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	if gjson.GetBytes(body, "token").String() != token {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	serverID := gjson.GetBytes(body, "server_id").String()
	if serverID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_id required"})
		return
	}

	if !serverIDRe.MatchString(serverID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
		return
	}

	now := time.Now()

	body, _ = sjson.DeleteBytes(body, "token")
	body, _ = sjson.SetBytes(
		body,
		"received_at",
		now.Format(time.RFC3339),
	)

	dir := filepath.Join(
		dataDir,
		serverID,
		now.Format("2006-01-02"),
	)

	if err := os.MkdirAll(dir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	filename := filepath.Join(
		dir,
		now.Format("150405.000000000")+".json",
	)

	if err := os.WriteFile(filename, body, 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func showStatus(c *gin.Context) {
	serverDirs, _ := os.ReadDir(dataDir)

	var htmlText strings.Builder

	htmlText.WriteString(`
		<html>
		<head>
			<meta charset="UTF-8">
			<meta name="viewport" content="width=device-width, initial-scale=1.0">
			<meta http-equiv="refresh" content="180">
			<title>Server Status</title>
			<style>
				body {
					background-color: black;
					filter: invert(1);
				}

				pre {
					font-family: Consolas, "Courier New", monospace;
					font-size: 13px;
					line-height: 1.1;
					margin: 8px 0 16px 0;
					overflow-x: auto;
					white-space: pre;
				}

				summary {
					cursor: pointer;
					font-weight: bold;
					margin-top: 10px;
				}
			</style>
		</head>
		<body>
			<h1>Server Status</h1>
	`)

	for _, serverDir := range serverDirs {
		if !serverDir.IsDir() {
			continue
		}

		serverID := serverDir.Name()

		filename := latestFile(
			filepath.Join(dataDir, serverID),
		)

		if filename == "" {
			continue
		}

		body, err := os.ReadFile(filename)
		if err != nil {
			continue
		}

		cpu := gjson.GetBytes(body, "cpu_percent").Float()
		receivedAt := gjson.GetBytes(body, "received_at").String()

		memoryPercent := gjson.GetBytes(body, "memory_percent").Float()
		memoryUsed := gjson.GetBytes(body, "memory_used_mb").Float()
		memoryTotal := gjson.GetBytes(body, "memory_total_mb").Float()

		memoryText := fmt.Sprintf("%.1f%%", memoryPercent)

		if memoryTotal > 0 {
			memoryText = fmt.Sprintf(
				"%.1f / %.1f GB (%.1f%%)",
				memoryUsed/1024,
				memoryTotal/1024,
				memoryPercent,
			)
		}

		hasGPU := gjson.GetBytes(body, "gpus.0").Exists()

		gpuText := "N/A"
		vramText := "N/A"
		tempText := "N/A"

		if hasGPU {
			gpu := gjson.GetBytes(body, "gpus.0.util_percent").Float()
			temp := gjson.GetBytes(body, "gpus.0.temperature_c").Float()

			vramUsed := gjson.GetBytes(body, "gpus.0.memory_used_mb").Float()
			vramTotal := gjson.GetBytes(body, "gpus.0.memory_total_mb").Float()

			gpuText = fmt.Sprintf("%.1f%%", gpu)
			tempText = fmt.Sprintf("%.1f°C", temp)

			if vramTotal > 0 {
				vramText = fmt.Sprintf(
					"%.1f / %.1f GB (%.1f%%)",
					vramUsed/1024,
					vramTotal/1024,
					vramUsed/vramTotal*100,
				)
			}
		}

		status := "OFFLINE"
		lastSeenText := receivedAt

		if receivedTime, err := time.Parse(time.RFC3339, receivedAt); err == nil {
			diff := time.Since(receivedTime)

			if diff < 10*time.Minute {
				status = "ONLINE"
			} else if diff < 30*time.Minute {
				status = "DELAYED"
			}

			if diff < time.Minute {
				lastSeenText = fmt.Sprintf("방금 전, %s", receivedAt)
			} else if diff < time.Hour {
				lastSeenText = fmt.Sprintf("%d분 전, %s", int(diff.Minutes()), receivedAt)
			} else {
				lastSeenText = fmt.Sprintf("%.1f시간 전, %s", diff.Hours(), receivedAt)
			}
		}

		history := loadHistory(serverID, historyWindow, historyMaxPoints)

		cpuPlot := makeAsciiGraph(
			"CPU 최근 6시간",
			collectHistoryValues(history, func(p historyPoint) float64 { return p.CPU }),
			0,
			100,
		)

		ramPlot := makeAsciiGraph(
			"RAM 최근 6시간",
			collectHistoryValues(history, func(p historyPoint) float64 { return p.RAM }),
			0,
			100,
		)

		gpuPlot := "GPU 최근 6시간\nN/A"
		tempPlot := "TEMP 최근 6시간\nN/A"

		if hasGPU {
			gpuPlot = makeAsciiGraph(
				"GPU 최근 6시간",
				collectGPUHistoryValues(history, func(p historyPoint) float64 { return p.GPU }),
				0,
				100,
			)

			tempPlot = makeAsciiGraph(
				"TEMP 최근 6시간",
				collectGPUHistoryValues(history, func(p historyPoint) float64 { return p.Temp }),
				20,
				95,
			)
		}

		fmt.Fprintf(
			&htmlText,
			`
			<hr>
			<h2>%s</h2>
			<p>%s</p>
			<p>CPU: %.1f%%</p>
			<p>RAM: %s</p>
			<p>GPU: %s</p>
			<p>VRAM: %s</p>
			<p>GPU 온도: %s</p>
			<p>마지막 수신: %s</p>

			<details>
				<summary>최근 6시간 그래프</summary>
				<pre>%s</pre>
				<pre>%s</pre>
				<pre>%s</pre>
				<pre>%s</pre>
			</details>
			`,
			html.EscapeString(serverID),
			html.EscapeString(status),
			cpu,
			html.EscapeString(memoryText),
			html.EscapeString(gpuText),
			html.EscapeString(vramText),
			html.EscapeString(tempText),
			html.EscapeString(lastSeenText),
			html.EscapeString(cpuPlot),
			html.EscapeString(ramPlot),
			html.EscapeString(gpuPlot),
			html.EscapeString(tempPlot),
		)
	}

	htmlText.WriteString(`
		</body>
		</html>
	`)

	c.Data(
		http.StatusOK,
		"text/html; charset=utf-8",
		[]byte(htmlText.String()),
	)
}

func latestFile(serverDir string) string {
	dateDirs, err := os.ReadDir(serverDir)
	if err != nil || len(dateDirs) == 0 {
		return ""
	}

	latestDateDir := dateDirs[len(dateDirs)-1]

	files, err := os.ReadDir(
		filepath.Join(serverDir, latestDateDir.Name()),
	)
	if err != nil || len(files) == 0 {
		return ""
	}

	latest := files[len(files)-1]

	return filepath.Join(
		serverDir,
		latestDateDir.Name(),
		latest.Name(),
	)
}

func loadHistory(serverID string, window time.Duration, maxPoints int) []historyPoint {
	serverPath := filepath.Join(dataDir, serverID)

	dateDirs, err := os.ReadDir(serverPath)
	if err != nil || len(dateDirs) == 0 {
		return nil
	}

	cutoff := time.Now().Add(-window)

	var points []historyPoint
	done := false

	for i := len(dateDirs) - 1; i >= 0 && !done; i-- {
		if !dateDirs[i].IsDir() {
			continue
		}

		datePath := filepath.Join(serverPath, dateDirs[i].Name())

		files, err := os.ReadDir(datePath)
		if err != nil || len(files) == 0 {
			continue
		}

		for j := len(files) - 1; j >= 0; j-- {
			if files[j].IsDir() {
				continue
			}

			filePath := filepath.Join(datePath, files[j].Name())

			body, err := os.ReadFile(filePath)
			if err != nil {
				continue
			}

			receivedAt := gjson.GetBytes(body, "received_at").String()

			receivedTime, err := time.Parse(time.RFC3339, receivedAt)
			if err != nil {
				continue
			}

			if receivedTime.Before(cutoff) {
				done = true
				break
			}

			gpuData := gjson.GetBytes(body, "gpus.0")
			hasGPU := gpuData.Exists()

			point := historyPoint{
				CPU:    gjson.GetBytes(body, "cpu_percent").Float(),
				RAM:    gjson.GetBytes(body, "memory_percent").Float(),
				HasGPU: hasGPU,
			}

			if hasGPU {
				point.GPU = gjson.GetBytes(body, "gpus.0.util_percent").Float()
				point.Temp = gjson.GetBytes(body, "gpus.0.temperature_c").Float()
			}

			points = append(points, point)
		}
	}

	for i, j := 0, len(points)-1; i < j; i, j = i+1, j-1 {
		points[i], points[j] = points[j], points[i]
	}

	return downsampleHistory(points, maxPoints)
}

func downsampleHistory(points []historyPoint, maxPoints int) []historyPoint {
	if maxPoints <= 0 || len(points) <= maxPoints {
		return points
	}

	result := make([]historyPoint, 0, maxPoints)

	for i := 0; i < maxPoints; i++ {
		start := i * len(points) / maxPoints
		end := (i + 1) * len(points) / maxPoints

		if end <= start {
			end = start + 1
		}

		var cpuSum, ramSum, gpuSum, tempSum float64
		count := 0
		gpuCount := 0

		for _, p := range points[start:end] {
			cpuSum += p.CPU
			ramSum += p.RAM
			count++

			if p.HasGPU {
				gpuSum += p.GPU
				tempSum += p.Temp
				gpuCount++
			}
		}

		point := historyPoint{
			CPU: cpuSum / float64(count),
			RAM: ramSum / float64(count),
		}

		if gpuCount > 0 {
			point.HasGPU = true
			point.GPU = gpuSum / float64(gpuCount)
			point.Temp = tempSum / float64(gpuCount)
		}

		result = append(result, point)
	}

	return result
}

func collectHistoryValues(points []historyPoint, selector func(historyPoint) float64) []float64 {
	values := make([]float64, 0, len(points))

	for _, p := range points {
		values = append(values, selector(p))
	}

	return values
}

func collectGPUHistoryValues(points []historyPoint, selector func(historyPoint) float64) []float64 {
	values := make([]float64, 0, len(points))

	for _, p := range points {
		if p.HasGPU {
			values = append(values, selector(p))
		}
	}

	return values
}

func makeAsciiGraph(title string, values []float64, minVal float64, maxVal float64) string {
	if len(values) == 0 {
		return title + "\nN/A"
	}

	graph := asciigraph.Plot(
		values,
		asciigraph.Caption(title),
		asciigraph.Height(8),
		asciigraph.Width(60),
		asciigraph.LowerBound(minVal),
		asciigraph.UpperBound(maxVal),
		asciigraph.Precision(1),
	)
	graph = normalizeGraphForMobile(graph)

	return graph
}