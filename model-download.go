package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

var chModelDownloadQueue = make(chan ModelDownloadQueueItem, 200)

type ProgressReader struct {
	io.Reader
	chProgress chan int
}

func (r ProgressReader) Read(dst []byte) (int, error) {
	n, err := r.Reader.Read(dst)
	r.chProgress <- n
	return n, err
}

func downloadModelRoutineImpl(appCtx AppContext, item ModelDownloadQueueItem) bool {
	file, err := os.Create(filepath.Join(appCtx.Config.ComfyuiLoraPath, item.Filename))
	if err != nil {
		log.Printf("failed to open file for download: %v\n", err)
		log.Printf("Download failed for: %v\n", item)
		return false
	}
	defer file.Close()

	req, err := http.NewRequest("GET", item.DownloadUrl, nil)
	if err != nil {
		log.Printf("failed to create request for model download: %v\n", err)
		log.Printf("Download failed for: %v\n", item)
		return false
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", appCtx.Config.CivitaiApiToken))

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("failed to send request for model download: %v\n", err)
		log.Printf("Download failed for: %v\n", item)
		return false
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		log.Printf("Civitai responded with %d for downloading model", res.StatusCode)
		log.Printf("Download filaed for: %v\n", item)
		return false
	}

	totalN, err := strconv.Atoi(res.Header.Get("Content-Length"))
	if err != nil {
		log.Printf("failed to read Content-Length header: %v\n", err)
		log.Printf("Download failed for: %v\n", item)
		return false
	}

	chProgress := make(chan int, 100)
	reader := ProgressReader{
		Reader:     res.Body,
		chProgress: chProgress,
	}
	chStopProgress := make(chan struct{})
	lastPrintedAt := time.Now()
	go func() {
		downloaded := 0
		for loop := true; loop; {
			select {
			case n := <-chProgress:
				downloaded += n
				now := time.Now()
				if now.Sub(lastPrintedAt) > 5*time.Second {
					log.Printf("Downloading %s: %.2f%%", item.Filename, float64(downloaded)/float64(totalN)*100)
					lastPrintedAt = now
				}
			case <-chStopProgress:
				loop = false
			}
		}
	}()
	defer func() {
		close(chStopProgress)
	}()

	if _, err := io.Copy(file, reader); err != nil {
		log.Printf("failed to download response body for model download: %v\n", err)
		log.Printf("Download failed for: %v\n", item)
		return false
	}

	log.Printf("Successfully downloaded model file: %s\n", item.Filename)

	if result, err := appCtx.db.Exec("DELETE FROM downloadWipV2 WHERE loraId = ?", item.LoraId); err != nil {
		log.Printf("[WARNING] failed to delete download wip record: %v\n", err)
		return false
	} else if n, err := result.RowsAffected(); err != nil {
		log.Printf("[WARNING] failed to get affected rows number: %v\n", err)
		return false
	} else if n == 0 {
		log.Printf("[WARNING] lora download WIP record is missing\n")
		return false
	}

	defaultCheckpoint, err := queryDefaultCheckpoint(appCtx.dbr)
	if err != nil {
		log.Printf("failed to query default checkpoint filename: %v\n", err)
		log.Printf("failed to enqueue %d\n", item.LoraId)
		return false
	} else if defaultCheckpoint == "" {
		log.Printf("default checkpoint is not defined\n")
		log.Printf("failed to enqueue %d\n", item.LoraId)
		return false
	}

	u := url.URL{
		Scheme: "http",
		Host:   appCtx.Config.ComfyuiEndpoint,
		Path:   "/api/queue",
	}
	q := u.Query()
	q.Set("loraId", strconv.Itoa(int(item.LoraId)))
	q.Set("checkpointFilename", defaultCheckpoint)
	u.RawQuery = q.Encode()
	if resp, err := http.Post(u.String(), "", nil); err != nil {
		log.Printf("failed to enqueue sample image: %v", err)
		log.Printf("failed to enqueue: %d\n", item.LoraId)
		return false
	} else if resp.StatusCode < 200 || resp.StatusCode > 300 {
		log.Printf("ComfyUI responded with status: %d", resp.StatusCode)
		log.Printf("failed to enqueue %d\n", item.LoraId)
		return false
	}
	return true
}

func downloadModelRoutine(
	appCtx AppContext,
	chDownloadQueue <-chan ModelDownloadQueueItem,
	chStopNotify <-chan struct{},
	chStoppedNotify chan<- struct{}) {
	lastModelId := 0
	for loop := true; loop; {
		select {
		case item := <-chDownloadQueue:
			success := false
			for i := 0; i < 5; i += 1 {
				success = success || downloadModelRoutineImpl(appCtx, item)
				if success {
					break
				}
				time.Sleep(2 * time.Second)
			}

			if !success {
				log.Printf("[WARNING] Download skipped for %v\n", item)
			} else if lastModelId != item.ModelId {
				delete(getModelResCache, lastModelId)
			}
			lastModelId = item.ModelId
		case <-chStopNotify:
			loop = false
		}
	}
	close(chStoppedNotify)
}
