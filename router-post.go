package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"

	"github.com/gin-gonic/gin"
)

var postMappings = map[string]func(AppContext) func(*gin.Context){
	"/api/prefill/:modelId": apiPrefill,
}

func initPostRouters(engine *gin.Engine, appCtx AppContext) {
	for path, router := range postMappings {
		engine.POST(path, router(appCtx))
	}
}

type ModelDownloadQueueItem struct {
	LoraId      int
	ModelId     int
	VersionId   int
	DownloadUrl string
	Filename    string
}

func apiPrefill(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		var u struct {
			ModelId int `uri:"modelId" binding:"required"`
		}
		var f struct {
			Versions string `form:"versions" binding:"required"`
		}
		if err := ctx.Bind(&f); err != nil {
			log.Printf("failed to bind form data: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		if err := ctx.BindUri(&u); err != nil {
			log.Printf("failed to bind uri data: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		var versions []string
		if err := json.Unmarshal([]byte(f.Versions), &versions); err != nil {
			log.Printf("failed to unmarshal version data: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		model, err := getCivitaiModel(u.ModelId)
		if err != nil {
			log.Printf("failed get civitai model: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		modelDownloadQueueItems := make([]ModelDownloadQueueItem, 0, 10)
		abort := true
		tx, err := appCtx.db.Begin()
		defer func() {
			if abort {
				tx.Rollback()
			}
		}()
		if err != nil {
			log.Printf("failed to start sqlite transaction: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		for _, modelVersion := range model.ModelVersions {
			if !slices.Contains(versions, modelVersion.Name) {
				continue
			}
			var downloadUrl string
			var filename string
			for _, file := range modelVersion.Files {
				if file.Type == "Model" {
					downloadUrl = file.DownloadUrl
					filename = fmt.Sprintf("%d-%d-%s", model.Id, modelVersion.Id, file.Name)
					break
				}
			}
			if len(downloadUrl) == 0 || len(filename) == 0 {
				log.Printf("[WARNING] A version of model lacks model download URL!")
				log.Printf("[WARNING] Model Name: %s\n", model.Name)
				log.Printf("[WARNING] Model Version: %s\n", modelVersion.Name)
				continue
			}
			stmt0 := "INSERT INTO lorasV2 ( name, url, civitaiModelId, civitaiVersionId, filename ) VALUES ( ?, ?, ?, ?, ? )"
			modelUrl := url.URL{
				Scheme: "https",
				Host:   "civitai.com",
				Path:   path.Join("/", "models", strconv.Itoa(modelVersion.Id)),
			}
			q := modelUrl.Query()
			q.Set("modelVersionId", strconv.Itoa(modelVersion.Id))
			modelUrl.RawQuery = q.Encode()

			stmt1 := "INSERT INTO downloadWipV2 ( loraId ) VALUES ( ? )"

			result, err := tx.Exec(stmt0, model.Name, modelUrl.String(), model.Id, modelVersion.Id, filename)
			if err != nil {
				log.Printf("failed to insert lora v2 record: %v\n", err)
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			}

			loraId, err := result.LastInsertId()
			if err != nil {
				log.Printf("failed to get inserted lora record id: %v\n", err)
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			} else if _, err := tx.Exec(stmt1, loraId); err != nil {
				log.Printf("failed to insert lora download wip v2 record: %v\n", err)
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			}
			modelDownloadQueueItems = append(modelDownloadQueueItems, ModelDownloadQueueItem{
				LoraId:      int(loraId),
				ModelId:     model.Id,
				VersionId:   modelVersion.Id,
				DownloadUrl: downloadUrl,
				Filename:    filename,
			})
		}
		if err := tx.Commit(); err != nil {
			log.Printf("failed to commit transaction: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		abort = false
		for _, item := range modelDownloadQueueItems {
			chModelDownloadQueue <- item
		}
	}
}
