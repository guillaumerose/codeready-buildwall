package main

import (
	"encoding/json"
	"net/http"
	"os"
	"time"

	log "github.com/sirupsen/logrus"
)

type woopraTrend struct {
	Table [][][][]interface{} `json:"table"`
}

func dayWorker() error {
	if _, ok := os.LookupEnv("WOOPRA_USER"); !ok {
		return nil
	}
	log.Infof("refreshing woopra")
	req, _ := http.NewRequest("GET", "https://www.woopra.com/rest/3.7/trends?project=prod.crc&report_id=tl3u3m7uoz", nil)
	req.SetBasicAuth(os.Getenv("WOOPRA_USER"), os.Getenv("WOOPRA_PASSWORD"))

	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	var trend woopraTrend
	if err := json.NewDecoder(res.Body).Decode(&trend); err != nil {
		return err
	}
	lock.Lock()
	defer lock.Unlock()

	byOS := make(map[string]float64)
	for _, t := range trend.Table[1] {
		os := t[0][0].([]interface{})[0].(string)
		val := t[0][1].([]interface{})[0].(float64)
		byOS[os] = val
	}
	total := trend.Table[2][0][0][0].(float64)
	stats = []float64{
		byOS["linux"],
		byOS["windows"],
		byOS["darwin"],
		total,
	}
	return nil
}
