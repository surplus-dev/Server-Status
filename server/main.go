package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"fmt"

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

	r.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "status server running")
	})

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