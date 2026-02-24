package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

var postMappings = map[string]func(AppContext) func(*gin.Context){
	"/api/lora":                     postApiLora,
	"/api/combination":              postApiCombination,
	"/api/combination-test":         postApiCombinationTest,
	"/api/v2/lora/:modelId":         postApiV2LoraModelId,
	"/api/v2/fix":                   postFix,
	"/api/v2/combination":           postApiV2Combination,
	"/api/v2/random-prompt/aibooru": postApiV2RandomPromptAiBooru,
}

func postFix(appCtx AppContext) func(*gin.Context) {
	return func(ctx *gin.Context) {
		ctx.Status(200)
	}

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

func handleManual(appCtx AppContext, ctx *gin.Context, modelId int, versionId int, promptLists [][]string, modelName string, versionName string, filename string) error {
	abort := true
	tx, err := appCtx.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to start sqlite transaction: %w", err)
	}
	defer func() {
		if abort {
			tx.Rollback()
		}
	}()

	modelUrl := url.URL{
		Scheme: "https",
		Host:   "civitai.com",
		Path:   path.Join("/", "models", strconv.Itoa(modelId)),
	}
	q := modelUrl.Query()
	q.Set("modelVersionId", strconv.Itoa(versionId))
	modelUrl.RawQuery = q.Encode()

	stmt0 := "INSERT INTO lorasV2 ( name, version, url, civitaiModelId, civitaiVersionId, filename ) VALUES ( ?, ?, ?, ?, ?, ? )"
	result, err := tx.Exec(stmt0, modelName, versionName, modelUrl.String(), modelId, versionId, filename)
	if err != nil {
		return fmt.Errorf("failed to insert lora v2 record: %w", err)
	}

	loraId, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get inserted lora record id: %w", err)
	}

	for promptListIndex, promptList := range promptLists {
		stmt0 := "INSERT INTO promptListsV2 ( loraId, promptListId ) VALUES ( ?, ? )"
		promptListId := promptListIndex + 1
		if _, err := tx.Exec(stmt0, loraId, promptListId); err != nil {
			return fmt.Errorf("failed to insert prompt list record: %w", err)
		}

		stmt1 := "INSERT INTO promptsV2 ( loraId, promptListId, seq, prompt ) VALUES ( ?, ?, ?, ? )"
		for promptIndex, prompt := range promptList {
			if _, err := tx.Exec(stmt1, loraId, promptListId, promptIndex+1, prompt); err != nil {
				return fmt.Errorf("failed to insert prompt record: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	abort = false

	defaultCheckpoint, err := queryDefaultCheckpoint(appCtx.dbr)
	if err != nil {
		log.Printf("failed to query default checkpoint filename: %v\n", err)
		log.Printf("failed to enqueue %d\n", loraId)
		return nil
	} else if defaultCheckpoint == "" {
		log.Printf("default checkpoint is not defined\n")
		log.Printf("failed to enqueue %d\n", loraId)
		return nil
	}

	u := url.URL{
		Scheme: "http",
		Host:   appCtx.Config.ComfyuiEndpoint,
		Path:   "/api/queue",
	}
	qq := u.Query()
	qq.Set("loraId", strconv.Itoa(int(loraId)))
	qq.Set("checkpointFilename", defaultCheckpoint)
	u.RawQuery = qq.Encode()
	if resp, err := http.Post(u.String(), "", nil); err != nil {
		log.Printf("failed to enqueue sample image: %v", err)
		log.Printf("failed to enqueue: %d\n", loraId)
		return nil
	} else if resp.StatusCode < 200 || resp.StatusCode > 300 {
		log.Printf("ComfyUI responded with status: %d", resp.StatusCode)
		log.Printf("failed to enqueue %d\n", loraId)
		return nil
	}

	return nil
}

func postApiV2LoraModelId(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		var u struct {
			ModelId int `uri:"modelId" binding:"required"`
		}
		var f struct {
			VersionIds  string `form:"versionIds" binding:"required"`
			PromptLists string `form:"prompts" binding:"required"`
		}
		if err := ctx.Bind(&f); err != nil {
			log.Printf("failed to bind form data: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if err := ctx.BindUri(&u); err != nil {
			log.Printf("failed to bind uri data: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		var versionIds []int
		var promptLists [][][]string
		if err := json.Unmarshal([]byte(f.VersionIds), &versionIds); err != nil {
			log.Printf("failed to unmarshal version data: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if err := json.Unmarshal([]byte(f.PromptLists), &promptLists); err != nil {
			log.Printf("failed to unmarshal prompt list data: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		if ctx.Query("manual") != "" {
			if err := handleManual(appCtx, ctx, u.ModelId, versionIds[0], promptLists[0], ctx.Query("modelName"), ctx.Query("versionName"), ctx.Query("filename")); err != nil {
				log.Printf("failed to handle manual submission: %v\n", err)
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			}
			ctx.Status(200)
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
		if err != nil {
			log.Printf("failed to start sqlite transaction: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		defer func() {
			if abort {
				tx.Rollback()
			}
		}()
		previewDownloadQueueItems := make([]PreviewDownloadItem, 0, 2)
		for _, modelVersion := range model.ModelVersions {
			versionIndex := slices.Index(versionIds, modelVersion.Id)
			if versionIndex == -1 {
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

			modelUrl := url.URL{
				Scheme: "https",
				Host:   "civitai.com",
				Path:   path.Join("/", "models", strconv.Itoa(model.Id)),
			}
			q := modelUrl.Query()
			q.Set("modelVersionId", strconv.Itoa(modelVersion.Id))
			modelUrl.RawQuery = q.Encode()

			stmt0 := "INSERT INTO lorasV2 ( name, version, url, civitaiModelId, civitaiVersionId, filename ) VALUES ( ?, ?, ?, ?, ?, ? )"
			result, err := tx.Exec(stmt0, model.Name, modelVersion.Name, modelUrl.String(), model.Id, modelVersion.Id, filename)
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
			}

			for promptListIndex, promptList := range promptLists[versionIndex] {
				stmt0 := "INSERT INTO promptListsV2 ( loraId, promptListId ) VALUES ( ?, ? )"
				promptListId := promptListIndex + 1
				if _, err := tx.Exec(stmt0, loraId, promptListId); err != nil {
					log.Printf("failed to insert prompt list record: %v\n", err)
					ctx.AbortWithStatus(http.StatusBadRequest)
					return
				}

				stmt1 := "INSERT INTO promptsV2 ( loraId, promptListId, seq, prompt ) VALUES ( ?, ?, ?, ? )"
				for promptIndex, prompt := range promptList {
					if _, err := tx.Exec(stmt1, loraId, promptListId, promptIndex+1, prompt); err != nil {
						log.Printf("failed to insert prompt record: %v\n", err)
						ctx.AbortWithStatus(http.StatusBadRequest)
						return
					}
				}
			}

			stmt2 := "INSERT INTO downloadWipV2 ( loraId ) VALUES ( ? )"
			if _, err := tx.Exec(stmt2, loraId); err != nil {
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

			count := 0
			for index := 0; index < len(modelVersion.Images) && count < 2; index += 1 {
				if modelVersion.Images[index].Type == "video" {
					continue
				}
				previewDownloadQueueItems = append(previewDownloadQueueItems, PreviewDownloadItem{
					ImageUrl:   modelVersion.Images[index].Url,
					LoraId:     int(loraId),
					PreviewSeq: count + 1,
				})
				count += 1
			}
		}
		if err := tx.Commit(); err != nil {
			log.Printf("failed to commit transaction: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		abort = false
		go func() {
			for _, item := range modelDownloadQueueItems {
				chModelDownloadQueue <- item
			}
			for _, item := range previewDownloadQueueItems {
				chPreviewDownloadQueue <- item
			}
		}()
	}
}

func postApiLora(appCtx AppContext) func(*gin.Context) {
	return func(ctx *gin.Context) {
		var f struct {
			Title    string `form:"title"`
			Url      string `form:"url"`
			Version  string `form:"version"`
			Filename string `form:"filename"`
			Prompts  string `form:"prompts"`
		}
		if err := ctx.Bind(&f); err != nil {
			log.Printf("failed to bind post lora form: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		urlpreview, err := readFormFile(ctx, "urlpreview")
		if err != nil {
			log.Printf("failed to read urlpreview form file: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		var prompts [][]string
		if err := json.Unmarshal([]byte(f.Prompts), &prompts); err != nil {
			log.Printf("failed to unmarshal prompts form data: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		abort := true
		tx, err := appCtx.db.Begin()
		if err != nil {
			log.Printf("failed to start transaction: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		defer func() {
			if abort {
				tx.Rollback()
			}
		}()

		loraId, err := insertLoraRow(tx, f.Title, f.Url, f.Version, f.Filename, prompts, urlpreview)
		if err != nil {
			log.Printf("failed to insert new lora row: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		u := url.URL{
			Scheme: "http",
			Host:   appCtx.Config.ComfyuiEndpoint,
			Path:   "/api/queue",
		}

		defaultCheckpoint, err := queryDefaultCheckpoint(tx)
		if err != nil {
			log.Printf("failed to query default checkpoint filename: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if defaultCheckpoint == "" {
			log.Printf("default checkpoint is not defined\n")
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		if err := tx.Commit(); err != nil {
			log.Printf("failed to commit transaction: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		abort = false

		q := u.Query()
		q.Set("loraId", strconv.Itoa(int(loraId)))
		q.Set("checkpointFilename", defaultCheckpoint)
		u.RawQuery = q.Encode()
		log.Println(u.String())
		if resp, err := http.Post(u.String(), "", nil); err != nil {
			log.Printf("failed to enqueue sample image: %v", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if resp.StatusCode < 200 || resp.StatusCode > 300 {
			log.Printf("ComfyUI responded with status: %d", resp.StatusCode)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		ctx.Status(http.StatusOK)
	}
}

func postApiCombination(appCtx AppContext) func(*gin.Context) {
	return func(ctx *gin.Context) {
		var f struct {
			Ids       string `form:"ids"`
			Strengths string `form:"strengths"`
			Prompts   string `form:"prompts"`
		}
		if err := ctx.Bind(&f); err != nil {
			log.Printf("failed to bind post combination form: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		var ids []int
		var strengths []int
		var prompts []string
		if err := json.Unmarshal([]byte(f.Ids), &ids); err != nil {
			log.Printf("failed to unmarshal ids into integer array: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if err := json.Unmarshal([]byte(f.Strengths), &strengths); err != nil {
			log.Printf("failed to unmarshal strengths into integer array: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if err := json.Unmarshal([]byte(f.Prompts), &prompts); err != nil {
			log.Printf("failed to unmarshal prompts into string array: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		abort := true
		tx, err := appCtx.db.Begin()
		if err != nil {
			log.Printf("failed to start transaction: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		defer func() {
			if abort {
				tx.Rollback()
			}
		}()

		stmt0Result, err := tx.Exec("INSERT INTO loraCombinations DEFAULT VALUES")
		if err != nil {
			log.Printf("failed to insert new lora combination: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		loraCombinationId, err := stmt0Result.LastInsertId()
		if err != nil {
			log.Printf("failed to get new lora combination ID: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		for index, loraId := range ids {
			stmt1 := `INSERT INTO loraCombinationComponents ( loraCombinationId, loraId, seq, strength ) VALUES ( ?, ?, ?, ? )`
			if _, err := tx.Exec(stmt1, loraCombinationId, loraId, index+1, strengths[index]); err != nil {
				log.Printf("failed to insert lora combination component: %v\n", err)
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			}
		}

		for index, prompt := range prompts {
			stmt2 := `INSERT INTO loraCombinationPrompts ( loraCombinationId, seq, prompt ) VALUES ( ?, ?, ? )`
			if _, err := tx.Exec(stmt2, loraCombinationId, index+1, prompt); err != nil {
				log.Printf("failed to insert lora combination prompt: %v\n", err)
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			}
		}

		if err := tx.Commit(); err != nil {
			log.Printf("failed to commit transaction: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		abort = false

		defaultCheckpoint, err := queryDefaultCheckpoint(appCtx.db)
		if err != nil {
			log.Printf("failed to query default checkpoint: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if defaultCheckpoint == "" {
			log.Printf("default checkpoint is not defined")
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		u := url.URL{
			Scheme: "http",
			Host:   appCtx.Config.ComfyuiEndpoint,
			Path:   "/api/queue-combination",
		}
		q := u.Query()
		q.Set("combinationId", strconv.Itoa(int(loraCombinationId)))
		q.Set("checkpointFilename", defaultCheckpoint)
		u.RawQuery = q.Encode()
		if resp, err := http.Post(u.String(), "", nil); err != nil {
			log.Printf("failed to enqueue sample image: %v", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if resp.StatusCode < 200 || resp.StatusCode > 300 {
			log.Printf("ComfyUI responded with status: %d", resp.StatusCode)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		ctx.Status(http.StatusOK)
	}
}

func postApiV2Combination(appCtx AppContext) func(*gin.Context) {
	return func(ctx *gin.Context) {
		var f struct {
			Ids       string `form:"ids"`
			Strengths string `form:"strengths"`
			Prompts   string `form:"prompts"`
		}
		if err := ctx.Bind(&f); err != nil {
			log.Printf("failed to bind post combination form: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		var ids []int
		var strengths []int
		var prompts []string
		if err := json.Unmarshal([]byte(f.Ids), &ids); err != nil {
			log.Printf("failed to unmarshal ids into integer array: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if err := json.Unmarshal([]byte(f.Strengths), &strengths); err != nil {
			log.Printf("failed to unmarshal strengths into integer array: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if err := json.Unmarshal([]byte(f.Prompts), &prompts); err != nil {
			log.Printf("failed to unmarshal prompts into string array: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		abort := true
		tx, err := appCtx.db.Begin()
		if err != nil {
			log.Printf("failed to start transaction: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		defer func() {
			if abort {
				tx.Rollback()
			}
		}()

		stmt0Result, err := tx.Exec("INSERT INTO loraCombinationsV2 DEFAULT VALUES")
		if err != nil {
			log.Printf("failed to insert new lora combination: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		loraCombinationId, err := stmt0Result.LastInsertId()
		if err != nil {
			log.Printf("failed to get new lora combination ID: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		for index, loraId := range ids {
			stmt1 := `INSERT INTO loraCombinationComponentsV2 ( loraCombinationId, loraId, seq, strength ) VALUES ( ?, ?, ?, ? )`
			if _, err := tx.Exec(stmt1, loraCombinationId, loraId, index+1, strengths[index]); err != nil {
				log.Printf("failed to insert lora combination component: %v\n", err)
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			}
		}

		for index, prompt := range prompts {
			stmt2 := `INSERT INTO loraCombinationPromptsV2 ( loraCombinationId, seq, prompt ) VALUES ( ?, ?, ? )`
			if _, err := tx.Exec(stmt2, loraCombinationId, index+1, prompt); err != nil {
				log.Printf("failed to insert lora combination prompt: %v\n", err)
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			}
		}

		if err := tx.Commit(); err != nil {
			log.Printf("failed to commit transaction: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		abort = false

		defaultCheckpoint, err := queryDefaultCheckpoint(appCtx.db)
		if err != nil {
			log.Printf("failed to query default checkpoint: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if defaultCheckpoint == "" {
			log.Printf("default checkpoint is not defined")
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		u := url.URL{
			Scheme: "http",
			Host:   appCtx.Config.ComfyuiEndpoint,
			Path:   "/api/queue-combination",
		}
		q := u.Query()
		q.Set("combinationId", strconv.Itoa(int(loraCombinationId)))
		q.Set("checkpointFilename", defaultCheckpoint)
		u.RawQuery = q.Encode()
		if resp, err := http.Post(u.String(), "", nil); err != nil {
			log.Printf("failed to enqueue sample image: %v", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if resp.StatusCode < 200 || resp.StatusCode > 300 {
			log.Printf("ComfyUI responded with status: %d", resp.StatusCode)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		ctx.Status(http.StatusOK)
	}
}

func postApiCombinationTest(appCtx AppContext) func(*gin.Context) {
	return func(ctx *gin.Context) {
		var f struct {
			Ids     string `form:"ids"`
			Prompts string `form:"prompts"`
		}
		if err := ctx.Bind(&f); err != nil {
			log.Printf("failed to bind form: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		var ids []int
		var prompts []string
		if err := json.Unmarshal([]byte(f.Ids), &ids); err != nil {
			log.Printf("failed to unmarshal ids into integer array: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if err := json.Unmarshal([]byte(f.Prompts), &prompts); err != nil {
			log.Printf("failed to unmarshal prompts into string array: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		abort := true
		tx, err := appCtx.db.Begin()
		if err != nil {
			log.Printf("failed to start lora combination transaction: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		defer func() {
			if abort {
				tx.Rollback()
			}
		}()

		stmt0Result, err := tx.Exec("INSERT INTO loraCombinationTests DEFAULT VALUES")
		if err != nil {
			log.Printf("failed to insert new lora combination test: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		testId, err := stmt0Result.LastInsertId()
		if err != nil {
			log.Printf("failed to get new lora combination test ID: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		for index, loraId := range ids {
			stmt1 := `INSERT INTO loraCombinationTestComponents ( loraCombinationTestId, componentSeq, loraId ) VALUES ( ?, ?, ? )`
			if _, err := tx.Exec(stmt1, testId, index+1, loraId); err != nil {
				log.Printf("failed to insert lora combination test component: %v\n", err)
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			}
		}

		for index, prompt := range prompts {
			stmt2 := `INSERT INTO loraCombinationTestPrompts ( loraCombinationTestId, promptSeq, prompt ) VALUES ( ?, ?, ? )`
			if _, err := tx.Exec(stmt2, testId, index+1, prompt); err != nil {
				log.Printf("failed to insert lora combination test prompt: %v\n", err)
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			}
		}

		strSteps := func(offset, stride, limit int) []int {
			stepNum := ((limit - offset) / stride) + 1
			steps := make([]int, stepNum)
			for i := 0; i < stepNum; i += 1 {
				steps[i] = offset + stride*i
			}
			return steps
		}(10, 10, 80)
		base := len(strSteps)
		currentEnum := make([]int, len(ids))
		trialIndex := 1
	foo:
		for {
			stmt3 := `INSERT INTO loraCombinationTestTrials ( loraCombinationTestId, trialIndex ) VALUES ( ?, ? )`
			if _, err := tx.Exec(stmt3, testId, trialIndex); err != nil {
				log.Printf("failed to insert combination test trial: %v\n", err)
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			}
			for componentIndex, n := range currentEnum {
				stmt4 := `INSERT INTO loraCombinationTestTrialParameters ( loraCombinationTestId, trialIndex, componentSeq, strength ) VALUES ( ?, ?, ?, ? )`
				if _, err := tx.Exec(stmt4, testId, trialIndex, componentIndex+1, strSteps[n]); err != nil {
					log.Printf("failed to insert combination test trial parameter: %v\n", err)
					ctx.AbortWithStatus(http.StatusBadRequest)
					return
				}
			}

			for i := range currentEnum {
				currentEnum[i] += 1
				if currentEnum[i] == base {
					if i == len(ids)-1 {
						break foo
					} else {
						currentEnum[i] = 0
					}
				} else {
					break
				}
			}
			trialIndex++
		}

		if err := tx.Commit(); err != nil {
			log.Printf("failed to commit lora combination transaction: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		abort = false

		rows, err := appCtx.dbr.Query("SELECT checkpointFilename FROM defaultCheckpoint LIMIT 1")
		if err != nil {
			log.Printf("failed to query default checkpoint filename: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if !rows.Next() {
			log.Printf("default checkpoint is not defined\n")
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		var defaultCheckpoint string
		if err := rows.Scan(&defaultCheckpoint); err != nil {
			log.Printf("failed to scan default checkpoint filename: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		u := url.URL{
			Scheme: "http",
			Host:   appCtx.Config.ComfyuiEndpoint,
			Path:   "/api/queue-combination-test",
		}
		q := u.Query()
		q.Set("testId", strconv.Itoa(int(testId)))
		q.Set("checkpointFilename", defaultCheckpoint)
		u.RawQuery = q.Encode()
		if resp, err := http.Post(u.String(), "", nil); err != nil {
			log.Printf("failed to enqueue sample image: %v", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if resp.StatusCode < 200 || resp.StatusCode > 300 {
			log.Printf("ComfyUI responded with status: %d", resp.StatusCode)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		ctx.Status(http.StatusOK)
	}
}

func postApiV2RandomPromptAiBooru(appCtx AppContext) func(*gin.Context) {
	return func(ctx *gin.Context) {
		var q struct {
			Id int `form:"id"`
		}
		if err := ctx.BindQuery(&q); err != nil {
			log.Printf("failed to bind query parameters: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		u := url.URL{
			Scheme: "https",
			Host:   "aibooru.online",
			Path:   fmt.Sprintf("/posts/%d.json", q.Id),
		}

		req, err := http.NewRequest("GET", u.String(), nil)
		if err != nil {
			log.Printf("failed to create HTTP request: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		res, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("failed to send HTTP request: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		defer res.Body.Close()

		if res.StatusCode != 200 {
			log.Printf("AI Booru responded with %d\n", res.StatusCode)
			return
		}

		resBodyRaw, err := io.ReadAll(res.Body)
		if err != nil {
			log.Printf("failed to read response body: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		var resJson struct {
			TagString string `json:"tag_string"`
		}
		if err := json.Unmarshal(resBodyRaw, &resJson); err != nil {
			log.Printf("failed to unmarshal response: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		prompts := strings.Split(resJson.TagString, " ")

		tx, err := appCtx.db.Begin()
		if err != nil {
			log.Printf("failed to start transaction: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		abort := true
		defer func() {
			if abort {
				tx.Rollback()
			}
		}()

		promptListId, err := insertRandomPromptsV2(tx, prompts)
		if err != nil {
			log.Printf("failed to insert random prompts: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		checkpointFilename, err := queryDefaultCheckpoint(tx)
		if err != nil {
			log.Printf("failed to query default checkpoint: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		combinationIds, err := queryLoraCombinationIds(tx)
		if err != nil {
			log.Printf("failed to query lora combination ids: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		if err := tx.Commit(); err != nil {
			log.Printf("failed to commit transaction: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		for _, combinationId := range combinationIds {
			u := url.URL{
				Scheme: "http",
				Host:   appCtx.Config.ComfyuiEndpoint,
				Path:   "/api/queue-combination-random-prompt",
			}
			q := u.Query()
			q.Set("combinationId", strconv.Itoa(combinationId))
			q.Set("checkpointFilename", checkpointFilename)
			q.Set("promptId", strconv.Itoa(promptListId))
			u.RawQuery = q.Encode()
			req, err := http.NewRequest("POST", u.String(), nil)
			if err != nil {
				log.Printf("failed to create HTTP request: %v\n", err)
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			}

			res, err := http.DefaultClient.Do(req)
			if err != nil {
				log.Printf("failed to send HTTP request: %v\n", err)
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			} else if res.StatusCode != 200 {
				log.Printf("Comfyui API responded with %d\n", res.StatusCode)
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			}
		}

		ctx.Status(200)
	}

}
