package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// キューメトリクスの定義
var queueMetrics = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "misskey_queue_length",
		Help: "Current length of Misskey queues",
	},
	[]string{"queue", "state"},
)

func getEnvVariable(key string) string {
	value := os.Getenv(key)
	if value == "" {
		fmt.Printf("Error: %s is not set\n", key)
		os.Exit(1) // 必須の環境変数がない場合は終了
	}
	return value
}

type QueueStats map[string]map[string]int

func fetchQueueStats() {
	apiKey := getEnvVariable("MISSKEY_API_KEY") // 環境変数からAPIキーを取得
	misskeyURL := getEnvVariable("MISSKEY_URL")

	// MisskeyのAPIに必要なリクエストボディ
	requestBody, err := json.Marshal(map[string]string{})
	if err != nil {
		fmt.Println("Error encoding JSON:", err)
		return
	}
	client := &http.Client{}
	req, err := http.NewRequest("POST", misskeyURL+"/api/admin/queue/stats", bytes.NewBuffer(requestBody))
	if err != nil {
		fmt.Println("Error creating request:", err)
		return
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error fetching queue stats:", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response body:", err)
		return
	}

	// 受け取ったJSONをそのまま出力
	// fmt.Println("Raw JSON Response:", string(body))

	var stats QueueStats
	if err := json.Unmarshal(body, &stats); err != nil {
		fmt.Println("Error unmarshalling JSON:", err)
		return
	}

	// メトリクスの更新
	for queue, states := range stats {
		for state, count := range states {
			queueMetrics.WithLabelValues(queue, state).Set(float64(count))
		}
	}
}

func main() {
	prometheus.MustRegister(queueMetrics)

	http.Handle("/metrics", promhttp.Handler())

	go func() {
		for {
			fetchQueueStats()
			time.Sleep(10 * time.Second) // 10秒ごとに更新
		}
	}()

	fmt.Println("Exporter running on :9100")
	http.ListenAndServe(":9100", nil)
}
