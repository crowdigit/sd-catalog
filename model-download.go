package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
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

func downloadModelRoutine(
	appCtx AppContext,
	chDownloadQueue <-chan ModelDownloadQueueItem,
	chStopNotify <-chan struct{},
	chStoppedNotify chan<- struct{}) {
	lastModelId := 0
	for loop := true; loop; {
		select {
		case item := <-chDownloadQueue:
			func() {
				file, err := os.Create(filepath.Join(appCtx.Config.ComfyuiLoraPath, item.Filename))
				if err != nil {
					log.Printf("failed to open file for download: %v\n", err)
					log.Printf("Download failed for: %v\n", item)
					return
				}
				defer file.Close()

				req, err := http.NewRequest("GET", item.DownloadUrl, nil)
				if err != nil {
					log.Printf("failed to create request for model download: %v\n", err)
					log.Printf("Download failed for: %v\n", item)
					return
				}
				req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", appCtx.Config.CivitaiApiToken))

				res, err := http.DefaultClient.Do(req)
				if err != nil {
					log.Printf("failed to send request for model download: %v\n", err)
					log.Printf("Download failed for: %v\n", item)
					return
				}
				defer res.Body.Close()

				totalN, err := strconv.Atoi(res.Header.Get("Content-Length"))
				if err != nil {
					log.Printf("failed to read Content-Length header: %v\n", err)
					log.Printf("Download failed for: %v\n", item)
					return
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
					return
				}

				log.Printf("Successfully downloaded model file: %s\n", item.Filename)

				if result, err := appCtx.db.Exec("DELETE FROM downloadWipV2 WHERE loraId = ?", item.LoraId); err != nil {
					log.Printf("[WARNING] failed to delete download wip record: %v\n", err)
					return
				} else if n, err := result.RowsAffected(); err != nil {
					log.Printf("[WARNING] failed to get affected rows number: %v\n", err)
					return
				} else if n == 0 {
					log.Printf("[WARNING] lora download WIP record is missing\n")
					return
				}
			}()
			if lastModelId != item.ModelId {
				delete(getModelResCache, lastModelId)
			}
			lastModelId = item.ModelId
		case <-chStopNotify:
			loop = false
		}
	}
	close(chStoppedNotify)
}
