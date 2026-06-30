package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var (
	token   = "change-this-token"
	dataDir = "./data"
	port    = "8080"
)

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

    token = config["token"]
	port = config["port"]
}

func main() {
    initJsonOpen()

	r := gin.Default()

	r.GET("/", showStatus)
	r.POST("/api/metrics", saveMetrics)

	r.Run(":" + port)

	fmt.Println("Run in :" + port)
}

func saveMetrics(c *gin.Context) {
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

	var html strings.Builder

	html.WriteString(`
		<html>
		<head>
			<meta charset="UTF-8">
			<meta http-equiv="refresh" content="180">
			<title>Server Status</title>
			<style>
				body {
					background-color: black;
					filter: invert(1);
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
		memory := gjson.GetBytes(body, "memory_percent").Float()
		gpu := gjson.GetBytes(body, "gpus.0.util_percent").Float()
		temp := gjson.GetBytes(body, "gpus.0.temperature_c").Float()
		receivedAt := gjson.GetBytes(body, "received_at").String()

		status := "OFFLINE"

		if receivedTime, err := time.Parse(time.RFC3339, receivedAt); err == nil {
			if time.Since(receivedTime) < 10*time.Minute {
				status = "ONLINE"
			}
		}

		fmt.Fprintf(
			&html,
			`
			<hr>
			<h2>%s</h2>
			<p>%s</p>
			<p>CPU: %.1f%%</p>
			<p>RAM: %.1f%%</p>
			<p>GPU: %.1f%%</p>
			<p>GPU 온도: %.1f°C</p>
			<p>마지막 수신: %s</p>
			`,
			serverID,
			status,
			cpu,
			memory,
			gpu,
			temp,
			receivedAt,
		)
	}

	html.WriteString(`
		</body>
		</html>
	`)

	c.Data(
		http.StatusOK,
		"text/html; charset=utf-8",
		[]byte(html.String()),
	)
}

func latestFile(serverDir string) string {
	dateDirs, err := os.ReadDir(serverDir)
	if err != nil || len(dateDirs) == 0 {
		return ""
	}

	// 날짜 폴더 이름이 YYYY-MM-DD이므로 마지막 폴더가 최신
	latestDateDir := dateDirs[len(dateDirs)-1]

	files, err := os.ReadDir(
		filepath.Join(serverDir, latestDateDir.Name()),
	)
	if err != nil || len(files) == 0 {
		return ""
	}

	// 파일 이름이 시각이므로 마지막 파일이 최신
	latest := files[len(files)-1]

	return filepath.Join(
		serverDir,
		latestDateDir.Name(),
		latest.Name(),
	)
}