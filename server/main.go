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
	"sort"
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

type gpuMetric struct {
	Index       int
	Util        float64
	MemoryUsed  float64
	MemoryTotal float64
	Temp        float64
}

type gpuHistoryPoint struct {
	Util float64
	VRAM float64
	Temp float64
}

type historyPoint struct {
	CPU  float64
	RAM  float64
	GPUs map[int]gpuHistoryPoint
}

type gpuAccumulator struct {
	UtilSum float64
	VRAMSum float64
	TempSum float64
	Count   int
}

func initJsonOpen() {
	jsonFile := "set.json"

	jsonValue, err := os.ReadFile(jsonFile)
	if err != nil {
		fmt.Println("set.json read error:", err)
		return
	}

	config := map[string]string{}

	if err := json.Unmarshal(jsonValue, &config); err != nil {
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
	c.Request.Body = http.MaxBytesReader(
		c.Writer,
		c.Request.Body,
		1<<20,
	)

	body, err := io.ReadAll(c.Request.Body)
	if err != nil || !json.Valid(body) {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": "invalid json"},
		)
		return
	}

	if gjson.GetBytes(body, "token").String() != token {
		c.JSON(
			http.StatusUnauthorized,
			gin.H{"error": "invalid token"},
		)
		return
	}

	serverID := gjson.GetBytes(body, "server_id").String()

	if serverID == "" {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": "server_id required"},
		)
		return
	}

	if !serverIDRe.MatchString(serverID) {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": "invalid server_id"},
		)
		return
	}

	now := time.Now()

	body, _ = sjson.DeleteBytes(
		body,
		"token",
	)

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
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}

	filename := filepath.Join(
		dir,
		now.Format("150405.000000000")+".json",
	)

	if err := os.WriteFile(filename, body, 0644); err != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}

	c.JSON(
		http.StatusOK,
		gin.H{"ok": true},
	)
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
					overflow-x: clip;
					white-space: pre;
				}

				summary {
					cursor: pointer;
					font-weight: bold;
					margin-top: 10px;
				}

				.gpu-block {
					margin-left: 12px;
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

		cpu := gjson.GetBytes(
			body,
			"cpu_percent",
		).Float()

		receivedAt := gjson.GetBytes(
			body,
			"received_at",
		).String()

		memoryPercent := gjson.GetBytes(
			body,
			"memory_percent",
		).Float()

		memoryUsed := gjson.GetBytes(
			body,
			"memory_used_mb",
		).Float()

		memoryTotal := gjson.GetBytes(
			body,
			"memory_total_mb",
		).Float()

		memoryText := fmt.Sprintf(
			"%.1f%%",
			memoryPercent,
		)

		if memoryTotal > 0 {
			memoryText = fmt.Sprintf(
				"%.1f / %.1f GB (%.1f%%)",
				memoryUsed/1024,
				memoryTotal/1024,
				memoryPercent,
			)
		}

		status := "OFFLINE"
		lastSeenText := receivedAt

		if receivedTime, err := time.Parse(
			time.RFC3339,
			receivedAt,
		); err == nil {
			diff := time.Since(receivedTime)

			if diff < 10*time.Minute {
				status = "ONLINE"
			} else if diff < 30*time.Minute {
				status = "DELAYED"
			}

			if diff < time.Minute {
				lastSeenText = fmt.Sprintf(
					"방금 전, %s",
					receivedAt,
				)
			} else if diff < time.Hour {
				lastSeenText = fmt.Sprintf(
					"%d분 전, %s",
					int(diff.Minutes()),
					receivedAt,
				)
			} else {
				lastSeenText = fmt.Sprintf(
					"%.1f시간 전, %s",
					diff.Hours(),
					receivedAt,
				)
			}
		}

		history := loadHistory(
			serverID,
			historyWindow,
			historyMaxPoints,
		)

		currentGPUs := parseCurrentGPUs(body)

		gpuIndexes := collectGPUIndexes(
			currentGPUs,
			history,
		)

		cpuPlot := makeAsciiGraph(
			"CPU 최근 6시간",
			collectHistoryValues(
				history,
				func(point historyPoint) float64 {
					return point.CPU
				},
			),
			0,
			100,
		)

		ramPlot := makeAsciiGraph(
			"RAM 최근 6시간",
			collectHistoryValues(
				history,
				func(point historyPoint) float64 {
					return point.RAM
				},
			),
			0,
			100,
		)

		gpuStatusHTML := makeGPUStatusHTML(
			gpuIndexes,
			currentGPUs,
		)

		gpuGraphsHTML := makeGPUGraphsHTML(
			gpuIndexes,
			history,
		)

		fmt.Fprintf(
			&htmlText,
			`
			<hr>

			<h2>%s</h2>

			<p>%s</p>
			<p>CPU: %.1f%%</p>
			<p>RAM: %s</p>

			%s

			<p>마지막 수신: %s</p>

			<details>
				<summary>최근 6시간 그래프</summary>

				<pre>%s</pre>
				<pre>%s</pre>

				%s
			</details>
			`,
			html.EscapeString(serverID),
			html.EscapeString(status),
			cpu,
			html.EscapeString(memoryText),
			gpuStatusHTML,
			html.EscapeString(lastSeenText),
			html.EscapeString(cpuPlot),
			html.EscapeString(ramPlot),
			gpuGraphsHTML,
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

func parseCurrentGPUs(body []byte) map[int]gpuMetric {
	result := make(map[int]gpuMetric)

	gpuArray := gjson.GetBytes(
		body,
		"gpus",
	)

	if !gpuArray.IsArray() {
		return result
	}

	for position, gpu := range gpuArray.Array() {
		index := position

		if gpu.Get("index").Exists() {
			index = int(
				gpu.Get("index").Int(),
			)
		}

		result[index] = gpuMetric{
			Index: index,

			Util: gpu.Get(
				"util_percent",
			).Float(),

			MemoryUsed: gpu.Get(
				"memory_used_mb",
			).Float(),

			MemoryTotal: gpu.Get(
				"memory_total_mb",
			).Float(),

			Temp: gpu.Get(
				"temperature_c",
			).Float(),
		}
	}

	return result
}

func collectGPUIndexes(
	currentGPUs map[int]gpuMetric,
	history []historyPoint,
) []int {
	indexSet := make(map[int]struct{})

	for index := range currentGPUs {
		indexSet[index] = struct{}{}
	}

	for _, point := range history {
		for index := range point.GPUs {
			indexSet[index] = struct{}{}
		}
	}

	indexes := make(
		[]int,
		0,
		len(indexSet),
	)

	for index := range indexSet {
		indexes = append(
			indexes,
			index,
		)
	}

	sort.Ints(indexes)

	return indexes
}

func makeGPUStatusHTML(
	gpuIndexes []int,
	currentGPUs map[int]gpuMetric,
) string {
	if len(gpuIndexes) == 0 {
		return `
			<p>GPU: N/A</p>
			<p>VRAM: N/A</p>
			<p>GPU 온도: N/A</p>
		`
	}

	var result strings.Builder

	for _, index := range gpuIndexes {
		gpu, exists := currentGPUs[index]

		result.WriteString(
			`<div class="gpu-block">`,
		)

		fmt.Fprintf(
			&result,
			"<h3>GPU %d</h3>",
			index,
		)

		if !exists {
			result.WriteString(
				"<p>현재 상태: N/A (이력만 존재)</p>",
			)

			result.WriteString("</div>")
			continue
		}

		vramText := "N/A"

		if gpu.MemoryTotal > 0 {
			vramText = fmt.Sprintf(
				"%.1f / %.1f GB (%.1f%%)",
				gpu.MemoryUsed/1024,
				gpu.MemoryTotal/1024,
				gpu.MemoryUsed/gpu.MemoryTotal*100,
			)
		}

		fmt.Fprintf(
			&result,
			"<p>사용률: %.1f%%</p>",
			gpu.Util,
		)

		fmt.Fprintf(
			&result,
			"<p>VRAM: %s</p>",
			html.EscapeString(vramText),
		)

		fmt.Fprintf(
			&result,
			"<p>온도: %.1f°C</p>",
			gpu.Temp,
		)

		result.WriteString("</div>")
	}

	return result.String()
}

func makeGPUGraphsHTML(
	gpuIndexes []int,
	history []historyPoint,
) string {
	if len(gpuIndexes) == 0 {
		return "<pre>GPU 최근 6시간\nN/A</pre>"
	}

	var result strings.Builder

	for _, index := range gpuIndexes {
		gpuPlot := makeAsciiGraph(
			fmt.Sprintf(
				"GPU %d 사용률 최근 6시간",
				index,
			),
			collectGPUHistoryValues(
				history,
				index,
				func(point gpuHistoryPoint) float64 {
					return point.Util
				},
			),
			0,
			100,
		)

		vramPlot := makeAsciiGraph(
			fmt.Sprintf(
				"GPU %d VRAM 최근 6시간",
				index,
			),
			collectGPUHistoryValues(
				history,
				index,
				func(point gpuHistoryPoint) float64 {
					return point.VRAM
				},
			),
			0,
			100,
		)

		tempPlot := makeAsciiGraph(
			fmt.Sprintf(
				"GPU %d TEMP 최근 6시간",
				index,
			),
			collectGPUHistoryValues(
				history,
				index,
				func(point gpuHistoryPoint) float64 {
					return point.Temp
				},
			),
			20,
			95,
		)

		fmt.Fprintf(
			&result,
			"<h3>GPU %d</h3>",
			index,
		)

		fmt.Fprintf(
			&result,
			"<pre>%s</pre>",
			html.EscapeString(gpuPlot),
		)

		fmt.Fprintf(
			&result,
			"<pre>%s</pre>",
			html.EscapeString(vramPlot),
		)

		fmt.Fprintf(
			&result,
			"<pre>%s</pre>",
			html.EscapeString(tempPlot),
		)
	}

	return result.String()
}

func latestFile(serverDir string) string {
	dateDirs, err := os.ReadDir(serverDir)
	if err != nil || len(dateDirs) == 0 {
		return ""
	}

	for i := len(dateDirs) - 1; i >= 0; i-- {
		if !dateDirs[i].IsDir() {
			continue
		}

		datePath := filepath.Join(
			serverDir,
			dateDirs[i].Name(),
		)

		files, err := os.ReadDir(datePath)
		if err != nil || len(files) == 0 {
			continue
		}

		for j := len(files) - 1; j >= 0; j-- {
			if files[j].IsDir() {
				continue
			}

			return filepath.Join(
				datePath,
				files[j].Name(),
			)
		}
	}

	return ""
}

func loadHistory(
	serverID string,
	window time.Duration,
	maxPoints int,
) []historyPoint {
	serverPath := filepath.Join(
		dataDir,
		serverID,
	)

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

		datePath := filepath.Join(
			serverPath,
			dateDirs[i].Name(),
		)

		files, err := os.ReadDir(datePath)
		if err != nil || len(files) == 0 {
			continue
		}

		for j := len(files) - 1; j >= 0; j-- {
			if files[j].IsDir() {
				continue
			}

			filePath := filepath.Join(
				datePath,
				files[j].Name(),
			)

			body, err := os.ReadFile(filePath)
			if err != nil {
				continue
			}

			receivedAt := gjson.GetBytes(
				body,
				"received_at",
			).String()

			receivedTime, err := time.Parse(
				time.RFC3339,
				receivedAt,
			)

			if err != nil {
				continue
			}

			if receivedTime.Before(cutoff) {
				done = true
				break
			}

			point := historyPoint{
				CPU: gjson.GetBytes(
					body,
					"cpu_percent",
				).Float(),

				RAM: gjson.GetBytes(
					body,
					"memory_percent",
				).Float(),

				GPUs: make(
					map[int]gpuHistoryPoint,
				),
			}

			gpuArray := gjson.GetBytes(
				body,
				"gpus",
			)

			if gpuArray.IsArray() {
				for position, gpu := range gpuArray.Array() {
					index := position

					if gpu.Get("index").Exists() {
						index = int(
							gpu.Get("index").Int(),
						)
					}

					vramUsed := gpu.Get(
						"memory_used_mb",
					).Float()

					vramTotal := gpu.Get(
						"memory_total_mb",
					).Float()

					vramPercent := 0.0

					if vramTotal > 0 {
						vramPercent = (vramUsed / vramTotal * 100)
					}

					point.GPUs[index] = gpuHistoryPoint{
						Util: gpu.Get(
							"util_percent",
						).Float(),

						VRAM: vramPercent,

						Temp: gpu.Get(
							"temperature_c",
						).Float(),
					}
				}
			}

			points = append(
				points,
				point,
			)
		}
	}

	for i, j := 0, len(points)-1; i < j; i, j = i+1, j-1 {
		points[i], points[j] = points[j], points[i]
	}

	return downsampleHistory(
		points,
		maxPoints,
	)
}

func downsampleHistory(
	points []historyPoint,
	maxPoints int,
) []historyPoint {
	if maxPoints <= 0 || len(points) <= maxPoints {
		return points
	}

	result := make(
		[]historyPoint,
		0,
		maxPoints,
	)

	for i := 0; i < maxPoints; i++ {
		start := i * len(points) / maxPoints
		end := (i + 1) * len(points) / maxPoints

		if end <= start {
			end = start + 1
		}

		var cpuSum float64
		var ramSum float64

		count := 0

		gpuSums := make(
			map[int]*gpuAccumulator,
		)

		for _, point := range points[start:end] {
			cpuSum += point.CPU
			ramSum += point.RAM
			count++

			for index, gpu := range point.GPUs {
				accumulator, exists := gpuSums[index]

				if !exists {
					accumulator = &gpuAccumulator{}
					gpuSums[index] = accumulator
				}

				accumulator.UtilSum += gpu.Util
				accumulator.VRAMSum += gpu.VRAM
				accumulator.TempSum += gpu.Temp
				accumulator.Count++
			}
		}

		point := historyPoint{
			CPU: cpuSum / float64(count),
			RAM: ramSum / float64(count),

			GPUs: make(
				map[int]gpuHistoryPoint,
			),
		}

		for index, accumulator := range gpuSums {
			if accumulator.Count == 0 {
				continue
			}

			point.GPUs[index] = gpuHistoryPoint{
				Util: accumulator.UtilSum /
					float64(accumulator.Count),

				VRAM: accumulator.VRAMSum /
					float64(accumulator.Count),

				Temp: accumulator.TempSum /
					float64(accumulator.Count),
			}
		}

		result = append(
			result,
			point,
		)
	}

	return result
}

func collectHistoryValues(
	points []historyPoint,
	selector func(historyPoint) float64,
) []float64 {
	values := make(
		[]float64,
		0,
		len(points),
	)

	for _, point := range points {
		values = append(
			values,
			selector(point),
		)
	}

	return values
}

func collectGPUHistoryValues(
	points []historyPoint,
	gpuIndex int,
	selector func(gpuHistoryPoint) float64,
) []float64 {
	values := make(
		[]float64,
		0,
		len(points),
	)

	for _, point := range points {
		gpu, exists := point.GPUs[gpuIndex]

		if !exists {
			continue
		}

		values = append(
			values,
			selector(gpu),
		)
	}

	return values
}

func makeAsciiGraph(
	title string,
	values []float64,
	minVal float64,
	maxVal float64,
) string {
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