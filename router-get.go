package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

var browseLoraPageLimit = 5
var browseCombinationLimit = 9
var civitaiApiEndpoint = "civitai.com"
var civitaiApiPrefix = "/api/v1"

func initGetRouters(engine *gin.Engine, appCtx AppContext) {
	for path, router := range getMappings {
		engine.GET(path, router(appCtx))
	}
}

var getMappings = map[string]func(AppContext) func(*gin.Context){
	"/":                                     getIndex,
	"/submit-lora":                          getSubmitLora,
	"/submit-lora-combination":              getSubmitLoraCombination,
	"/submit-checkpoint":                    getSubmitCheckpoint,
	"/browse-lora":                          getBrowseLora,
	"/browse-combination":                   getBrowseCombination,
	"/lora/:loraId":                         getLoraByLoraId,
	"/combination/:combinationId":           getCombinationByCombinationId,
	"/prefill":                              getPrefill,
	"/test-combination":                     getTestCombination,
	"/api/browse/lora":                      apiBrowseLora,
	"/api/browse/lora/:page":                apiBrowseLoraPage,
	"/api/browse/combination":               apiBrowseCombination,
	"/api/browse/combination/:page":         apiBrowseCombinationPage,
	"/api/lora/:loraId":                     apiLoraLoraId,
	"/api/lora/:loraId/urlpreview":          apiLoraLoraIdUrlpreview,
	"/api/lora/:loraId/preview/:sampleType": apiLoraLoraIdPreviewSampleImage,
	"/api/lora/:loraId/sampleImage/:checkpointFilename/:promptlistId/:sampleType": apiLoraLoraIdSampleImageCheckpointFilenamePromtlistIdSampleType,
	"/api/combination/:combinationId/sampleImage/:checkpointFilename/:sampleType": apiCombinationCombinationIdSampleImageCheckpointFilenameSampleType,
	"/api/combination/:combinationId/preview/:sampleType":                         apiCombinationCombinationIdPreviewSampleType,
	"/api/prefill/:modelId/version":                                               getApiPrefillModelIdVersion,
}

type CivitaiModelVersion struct {
	Id           int      `json:"id"`
	Name         string   `json:"name"`
	BaseModel    string   `json:"baseModel"`
	TrainedWords []string `json:"trainedWords"`
	Files        []struct {
		Name        string `json:"name"`
		Type        string `json:"type"`
		DownloadUrl string `json:"downloadUrl"`
	} `json:"files"`
	Images []struct {
		Url string `json:"url"`
	} `json:"images"`
}

type CivitaiGetModelResponse struct {
	Id            int                   `json:"id"`
	Name          string                `json:"name"`
	ModelVersions []CivitaiModelVersion `json:"modelVersions"`
}

var getModelResCache = make(map[int]CivitaiGetModelResponse)

func getModel() {
}

func getIndex(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		ctx.Header("Content-Type", "text/html")
		ctx.String(http.StatusOK, indexHtml)
	}
}

func getSubmitLora(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		ctx.Header("Content-Type", "text/html")
		ctx.String(http.StatusOK, submitLoraHtml)
	}
}

func getSubmitLoraCombination(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		ctx.Header("Content-Type", "text/html")
		ctx.String(http.StatusOK, submitLoraCombinationHtml)
	}
}

func getSubmitCheckpoint(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		ctx.Header("Content-Type", "text/html")
		ctx.String(http.StatusOK, submitCheckpointHtml)
	}
}

func getBrowseLora(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		ctx.Header("Content-Type", "text/html")
		ctx.String(http.StatusOK, browseLoraHtml)
	}
}

func getBrowseCombination(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		ctx.Header("Content-Type", "text/html")
		ctx.String(http.StatusOK, browseCombinationHtml)
	}
}

func getTestCombination(appCtx AppContext) func(*gin.Context) {
	return func(ctx *gin.Context) {
		ctx.Header("Content-Type", "text/html")
		ctx.String(http.StatusOK, testCombinationHtml)
	}
}

func getLoraByLoraId(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		q := struct {
			LoraId int `uri:"loraId" binding:"required"`
		}{}
		if err := ctx.BindUri(&q); err != nil {
			log.Printf("uri binding failed: %v\n", err)
			return
		}

		lora, err := queryLora(appCtx.db, q.LoraId)
		if lora == nil {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}

		type AvailableSampleImageDatum struct {
			PromptListItem
			AvailableSampleImageTypes [][]int
		}
		type CatalogDatum struct {
			CheckpointRow
			AvailableSampleImageData []AvailableSampleImageDatum
		}

		checkpoints, err := queryCheckpoints(appCtx.db)
		if err != nil {
			log.Printf("failed to query checkpoints: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		promptList, err := queryLoraPrompts(appCtx.db, q.LoraId)
		if err != nil {
			log.Printf("failed to query prompts: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		type promptListItem struct {
			Id     int
			String string
		}
		prompts := make([]promptListItem, len(promptList))
		for index, promptListItem := range promptList {
			prompts[index].Id = promptListItem.PromptListId
			promptsStr := strings.Join(promptListItem.Prompts, ", ")
			if len(promptsStr) == 0 {
				prompts[index].String = "(empty)"
			} else {
				prompts[index].String = promptsStr
			}
		}

		type SampleRow []int
		type SampleRows []SampleRow
		sampleRowLimit := 3

		checkpointPromptsSamples := make([][]SampleRows, 0, len(checkpoints))
		for _, checkpoint := range checkpoints {
			promptsSamples := make([]SampleRows, 0, len(prompts))
			for _, promptListItem := range promptList {
				sampleImageTypes, err := queryAvailableSampleImageTypes(appCtx.db, q.LoraId, checkpoint.CheckpointFilename, promptListItem.PromptListId)
				if err != nil {
					log.Printf("failed to query available sample image types: %v\n", err)
					ctx.AbortWithStatus(http.StatusBadRequest)
					return
				}
				sampleRows := make(SampleRows, (len(sampleImageTypes)+sampleRowLimit-1)/sampleRowLimit)
				for index := range sampleRows {
					sampleRows[index] = make(SampleRow, 0, 3)
				}
				for index, sampleImageType := range sampleImageTypes {
					row := index / 3
					sampleRows[row] = append(sampleRows[row], sampleImageType)
				}
				promptsSamples = append(promptsSamples, sampleRows)
			}
			checkpointPromptsSamples = append(checkpointPromptsSamples, promptsSamples)
		}

		prev, next, err := queryLoraRelativeBrowseData(appCtx.db, q.LoraId)
		if err != nil {
			log.Printf("failed to query lora relative browse data: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		var loraHtmlInstBuf bytes.Buffer
		if err := appCtx.htmlTemplates.loraHtml.Execute(&loraHtmlInstBuf, struct {
			LoraRow
			LoraId      int
			Checkpoints []CheckpointRow
			Prompts     []promptListItem
			SampleRows  [][]SampleRows
			PrevLoraId  *int
			NextLoraId  *int
		}{
			LoraRow:     *lora,
			LoraId:      q.LoraId,
			Checkpoints: checkpoints,
			Prompts:     prompts,
			SampleRows:  checkpointPromptsSamples,
			PrevLoraId:  prev,
			NextLoraId:  next,
		}); err != nil {
			log.Printf("failed to instantiate lora html template: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		ctx.Header("Content-Type", "text/html")
		ctx.Data(http.StatusOK, "text/html", loraHtmlInstBuf.Bytes())
	}
}

func getCombinationByCombinationId(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		q := struct {
			CombinationId int `uri:"combinationId" binding:"required"`
		}{}
		if err := ctx.BindUri(&q); err != nil {
			log.Printf("uri binding failed: %v\n", err)
			return
		}

		components, err := queryCombinationComponents(appCtx.db, q.CombinationId)
		if err != nil {
			log.Printf("failed to query lora components: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		checkpoints, err := queryCheckpoints(appCtx.db)
		if err != nil {
			log.Printf("failed to query checkpoints: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		prompts, err := queryCombinationPrompts(appCtx.db, q.CombinationId)
		if err != nil {
			log.Printf("failed to query prompts: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		sampleTypes := make([][][]int, 0, 1)
		for _, checkpoint := range checkpoints {
			stmt := `SELECT sampleType
		FROM loraCombinationSampleImages
		WHERE
		loraCombinationSampleImages.loraCombinationId = ?
		AND checkpointFilename = ?
		ORDER BY sampleType ASC`
			rows, err := appCtx.db.Query(stmt, q.CombinationId, checkpoint.CheckpointFilename)
			if err != nil {
				log.Printf("failed to query available sample images: %v\n", err)
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			}
			sampleTypeRows := make([][]int, 0, 9)
			sampleTypeRows = append(sampleTypeRows, make([]int, 0, 3))
			for rows.Next() {
				var sampleType int
				if err := rows.Scan(&sampleType); err != nil {
					log.Printf("failed to scan available sample images: %v\n", err)
					ctx.AbortWithStatus(http.StatusBadRequest)
					return
				}
				tail := sampleTypeRows[len(sampleTypeRows)-1]
				if len(tail) == 3 {
					tail = make([]int, 0, 3)
					sampleTypeRows = append(sampleTypeRows, tail)
				}
				tail = append(tail, sampleType)
				sampleTypeRows[len(sampleTypeRows)-1] = tail
			}
			sampleTypes = append(sampleTypes, sampleTypeRows)
		}

		promptsJoint := strings.Join(prompts, ", ")

		prev, next, err := queryCombinationRelativeBrowseData(appCtx.db, q.CombinationId)
		if err != nil {
			log.Printf("failed to query lora relative browse data: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		if prev == nil && next == nil {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}

		var combinationHtmlInstBuf bytes.Buffer
		if err := appCtx.htmlTemplates.combinationHtml.Execute(&combinationHtmlInstBuf, struct {
			ToFloat           func(int) float64
			CombinationId     int
			Components        []LoraCombinationsComponent
			Checkpoints       []CheckpointRow
			SampleTypes       [][][]int
			Prompts           string
			PrevCombinationId *int
			NextCombinationId *int
		}{
			ToFloat:           func(n int) float64 { return 0.01 * float64(n) },
			CombinationId:     q.CombinationId,
			Components:        components,
			Checkpoints:       checkpoints,
			SampleTypes:       sampleTypes,
			Prompts:           promptsJoint,
			PrevCombinationId: prev,
			NextCombinationId: next,
		}); err != nil {
			log.Printf("failed to instantiate lora html template: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		ctx.Header("Content-Type", "text/html")
		ctx.Data(http.StatusOK, "text/html", combinationHtmlInstBuf.Bytes())
	}
}

func apiBrowseLora(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		rows, err := appCtx.db.Query("SELECT COUNT(*) AS num FROM loras")
		if err != nil {
			log.Printf("failed to select lora IDs: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		rows.Next()
		var num int
		if err := rows.Scan(&num); err != nil {
			log.Printf("failed to scan lora ID from row: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"maxPage": (num - 1) / browseLoraPageLimit})
	}
}

func apiBrowseLoraPage(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		page, err := strconv.Atoi(ctx.Param("page"))
		if err != nil {
			log.Printf("failed to parse page into integer: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		offset := browseLoraPageLimit * page
		rows, err := appCtx.db.Query("SELECT loraId, name, version FROM loras ORDER BY loraId ASC LIMIT ? OFFSET ?", browseLoraPageLimit, offset)
		if err != nil {
			log.Printf("failed to select lora IDs: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		loraIds := make([]int, 0, browseLoraPageLimit)
		names := make([]string, 0, browseLoraPageLimit)
		versions := make([]string, 0, browseLoraPageLimit)
		for rows.Next() {
			var loraId int64
			var name string
			var version string
			if err := rows.Scan(&loraId, &name, &version); err != nil {
				log.Printf("failed to scan lora ID from row: %v\n", err)
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			}
			loraIds = append(loraIds, int(loraId))
			names = append(names, name)
			versions = append(versions, version)
		}
		ctx.JSON(http.StatusOK, gin.H{
			"loraIds":  loraIds,
			"names":    names,
			"versions": versions,
		})
	}
}

func apiBrowseCombination(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		rows, err := appCtx.db.Query("SELECT COUNT(*) AS num FROM loraCombinations")
		if err != nil {
			log.Printf("failed to select combination IDs: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		rows.Next()
		var num int
		if err := rows.Scan(&num); err != nil {
			log.Printf("failed to scan combination ID from row: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"maxPage": (num - 1) / browseCombinationLimit})
	}
}

func apiBrowseCombinationPage(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		page, err := strconv.Atoi(ctx.Param("page"))
		if err != nil {
			log.Printf("failed to parse page into integer: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		offset := browseCombinationLimit * page
		rows, err := appCtx.db.Query("SELECT loraCombinationId FROM loraCombinations ORDER BY loraCombinationId ASC LIMIT ? OFFSET ?", browseCombinationLimit, offset)
		if err != nil {
			log.Printf("failed to select combination IDs: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		combinationIds := make([]int, 0, browseCombinationLimit)
		for rows.Next() {
			var combinationId int64
			if err := rows.Scan(&combinationId); err != nil {
				log.Printf("failed to scan combination ID from row: %v\n", err)
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			}
			combinationIds = append(combinationIds, int(combinationId))
		}
		ctx.JSON(http.StatusOK, gin.H{
			"combinationIds": combinationIds,
		})
	}
}

func apiLoraLoraId(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		loraId, err := strconv.Atoi(ctx.Param("loraId"))
		if err != nil {
			log.Printf("failed to parse lora ID into integer: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		lora, err := queryLora(appCtx.db, loraId)
		if err != nil {
			log.Printf("failed to query lora: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		ctx.Header("Cache-Control", "public, max-age=604800")
		ctx.JSON(http.StatusOK, lora)
	}
}

func apiLoraLoraIdUrlpreview(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		loraId, err := strconv.Atoi(ctx.Param("loraId"))
		if err != nil {
			log.Printf("failed to parse lora ID into integer: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		rows, err := appCtx.db.Query("SELECT urlpreview FROM loras WHERE loraId = ?", loraId)
		if err != nil {
			log.Printf("failed to select lora: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		if !rows.Next() {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}

		var urlpreview []byte
		if err := rows.Scan(&urlpreview); err != nil {
			ctx.AbortWithStatus(http.StatusBadRequest)
			log.Printf("failed to scan urlpreview image from query result: %v\n", err)
			return
		}

		ctx.Header("Cache-Control", "public, max-age=604800")
		ctx.Data(200, "image/png", urlpreview)
	}
}

func apiLoraLoraIdPreviewSampleImage(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		loraId, err := strconv.Atoi(ctx.Param("loraId"))
		if err != nil {
			log.Printf("failed to parse lora ID into integer: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		sampleType, err := strconv.Atoi(ctx.Param("sampleType"))
		if err != nil {
			log.Printf("failed to parse sample type into integer: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		defaultCheckpoint, err := queryDefaultCheckpoint(appCtx.db)
		if err != nil {
			log.Printf("failed to query default checkpoint: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		defaultPromptListId, err := queryDefaultPromptListId(appCtx.db, loraId)
		if err != nil {
			log.Printf("failed to query default prompt list ID: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		rows, err := appCtx.db.Query("SELECT sampleImage FROM sampleImages WHERE loraId = ? AND promptListId = ? AND checkpointFilename = ? AND sampleType = ?", loraId, defaultPromptListId, defaultCheckpoint, sampleType)
		if err != nil {
			log.Printf("failed to query default sample image: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		if !rows.Next() {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}
		var image []byte
		if err := rows.Scan(&image); err != nil {
			ctx.AbortWithStatus(http.StatusBadRequest)
			log.Printf("failed to scan sample image from query result: %v\n", err)
			return
		}

		ctx.Header("Cache-Control", "public, max-age=604800")
		ctx.Data(200, "image/png", image)
	}
}

func apiLoraLoraIdSampleImageCheckpointFilenamePromtlistIdSampleType(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		loraId, err := strconv.Atoi(ctx.Param("loraId"))
		if err != nil {
			log.Printf("failed to parse lora ID into integer: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		checkpointFilename := ctx.Param("checkpointFilename")
		promptListId, err := strconv.Atoi(ctx.Param("promptlistId"))
		if err != nil {
			log.Printf("failed to parse prompt list ID into integer: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		sampleType, err := strconv.Atoi(ctx.Param("sampleType"))
		if err != nil {
			log.Printf("failed to parse sample type into integer: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		rows, err := appCtx.db.Query(
			`SELECT sampleImage FROM sampleImages WHERE loraId = ? AND promptListId = ? AND checkpointFilename = ? AND sampleType = ? LIMIT 1`,
			loraId, promptListId, checkpointFilename, sampleType)
		if err != nil {
			log.Printf("failed to query sample image: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		if !rows.Next() {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}
		var image []byte
		if err := rows.Scan(&image); err != nil {
			ctx.AbortWithStatus(http.StatusBadRequest)
			log.Printf("failed to scan sample image: %v\n", err)
			return
		}

		ctx.Header("Cache-Control", "public, max-age=604800")
		ctx.Data(200, "image/png", image)
	}
}

func apiCombinationCombinationIdSampleImageCheckpointFilenameSampleType(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		combinationId, err := strconv.Atoi(ctx.Param("combinationId"))
		if err != nil {
			log.Printf("failed to parse combination ID into integer: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		checkpointFilename := ctx.Param("checkpointFilename")
		sampleType, err := strconv.Atoi(ctx.Param("sampleType"))
		if err != nil {
			log.Printf("failed to parse sample type into integer: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		rows, err := appCtx.db.Query(
			`SELECT sampleImage FROM loraCombinationSampleImages WHERE loraCombinationId = ? AND checkpointFilename = ? AND sampleType = ? LIMIT 1`,
			combinationId, checkpointFilename, sampleType)
		if err != nil {
			log.Printf("failed to query sample image: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		if !rows.Next() {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}
		var image []byte
		if err := rows.Scan(&image); err != nil {
			ctx.AbortWithStatus(http.StatusBadRequest)
			log.Printf("failed to scan sample image: %v\n", err)
			return
		}

		ctx.Header("Cache-Control", "public, max-age=604800")
		ctx.Data(200, "image/png", image)
	}
}

func apiCombinationCombinationIdPreviewSampleType(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		combinationId, err := strconv.Atoi(ctx.Param("combinationId"))
		if err != nil {
			log.Printf("failed to parse combination ID into integer: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		sampleType, err := strconv.Atoi(ctx.Param("sampleType"))
		if err != nil {
			log.Printf("failed to parse sample type into integer: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		defaultCheckpoint, err := queryDefaultCheckpoint(appCtx.db)
		if err != nil {
			log.Printf("failed to query default checkpoint: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		rows, err := appCtx.db.Query("SELECT sampleImage FROM loraCombinationSampleImages WHERE loraCombinationId = ? AND checkpointFilename = ? AND sampleType = ?", combinationId, defaultCheckpoint, sampleType)
		if err != nil {
			log.Printf("failed to query default sample image: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		if !rows.Next() {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}
		var image []byte
		if err := rows.Scan(&image); err != nil {
			ctx.AbortWithStatus(http.StatusBadRequest)
			log.Printf("failed to scan sample image from query result: %v\n", err)
			return
		}

		ctx.Header("Cache-Control", "public, max-age=604800")
		ctx.Data(200, "image/png", image)
	}
}

func getPrefill(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		ctx.Header("Content-Type", "text/html")
		ctx.String(http.StatusOK, prefillHtml)
	}
}

func getCivitaiModel(modelId int) (CivitaiGetModelResponse, error) {
	if cachedRes, cached := getModelResCache[modelId]; cached {
		return cachedRes, nil
	}
	getModelUrl := url.URL{
		Scheme: "https",
		Host:   civitaiApiEndpoint,
		Path:   path.Join(civitaiApiPrefix, "models", fmt.Sprintf("%d", modelId)),
	}

	getModelReq, err := http.NewRequest("GET", getModelUrl.String(), nil)
	if err != nil {
		return CivitaiGetModelResponse{}, fmt.Errorf("failed to create civitai model request: %w", err)
	}

	getModelRes, err := http.DefaultClient.Do(getModelReq)
	if err != nil {
		return CivitaiGetModelResponse{}, fmt.Errorf("failed to send civitai model request: %w", err)
	} else if getModelRes.StatusCode < 200 || getModelRes.StatusCode >= 300 {
		return CivitaiGetModelResponse{}, fmt.Errorf("civitai responded with %d", getModelRes.StatusCode)
	}

	getModelRespBody, err := io.ReadAll(getModelRes.Body)
	if err != nil {
		return CivitaiGetModelResponse{}, fmt.Errorf("failed to read response body from civitai: %w", err)
	}

	var model CivitaiGetModelResponse
	if err := json.Unmarshal(getModelRespBody, &model); err != nil {
		return CivitaiGetModelResponse{}, fmt.Errorf("failed to parse response body from civitai: %w", err)
	}

	return model, nil
}

func getApiPrefillModelIdVersion(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		var p struct {
			ModelId int `uri:"modelId" binding:"required"`
		}
		if err := ctx.BindUri(&p); err != nil {
			log.Printf("failed to bind uri parameters: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		model, err := getCivitaiModel(p.ModelId)
		if err != nil {
			log.Printf("failed to get civitai model: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		versions := make([]string, len(model.ModelVersions))
		for index, modelVersion := range model.ModelVersions {
			versions[index] = modelVersion.Name
		}
		ctx.JSON(http.StatusOK, versions)
	}
}
