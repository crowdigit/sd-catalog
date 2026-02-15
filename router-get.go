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
	"/submit-lora-v2":                       getSubmitLoraV2,
	"/submit-lora-combination":              getSubmitLoraCombination,
	"/submit-checkpoint":                    getSubmitCheckpoint,
	"/browse-lora":                          getBrowseLora,
	"/browse-combination":                   getBrowseCombination,
	"/lora/:loraId":                         getLoraByLoraId,
	"/combination/:combinationId":           getCombinationByCombinationId,
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
	"/api/v2/civitai/model/:modelId/version":                                      getApiV2CivitaiModelModelIdVersion,
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

		lora, err := queryLora(appCtx.dbr, q.LoraId)
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

		checkpoints, err := queryCheckpoints(appCtx.dbr)
		if err != nil {
			log.Printf("failed to query checkpoints: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		promptList, err := queryLoraPrompts(appCtx.dbr, q.LoraId)
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
				sampleImageTypes, err := queryAvailableSampleImageTypes(appCtx.dbr, q.LoraId, checkpoint.CheckpointFilename, promptListItem.PromptListId)
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

		prev, next, err := queryLoraRelativeBrowseData(appCtx.dbr, q.LoraId)
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

		components, err := queryCombinationComponents(appCtx.dbr, q.CombinationId)
		if err != nil {
			log.Printf("failed to query lora components: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		checkpoints, err := queryCheckpoints(appCtx.dbr)
		if err != nil {
			log.Printf("failed to query checkpoints: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		prompts, err := queryCombinationPrompts(appCtx.dbr, q.CombinationId)
		if err != nil {
			log.Printf("failed to query prompts: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		sampleTypes := make([][][]int, 0, 1)
		const rowSize = 3
		for _, checkpoint := range checkpoints {
			ts, err := queryLoraCombinationAvailableSampleTypes(appCtx.dbr, q.CombinationId, checkpoint.CheckpointFilename)
			if err != nil {
				log.Printf("failed to query available sample types: %v\n", err)
				ctx.AbortWithStatus(http.StatusBadRequest)
				return
			}
			rowNum := (len(ts) + rowSize - 1) / 3
			rows := make([][]int, rowNum)
			for i := 0; i < rowNum; i += 1 {
				rows[i] = ts[i*rowSize : min((i+1)*rowSize, len(ts))]
			}
			sampleTypes = append(sampleTypes, rows)
		}

		promptsJoint := strings.Join(prompts, ", ")

		prev, next, err := queryCombinationRelativeBrowseData(appCtx.dbr, q.CombinationId)
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
		count, err := queryLoraCount(appCtx.dbr)
		if err != nil {
			log.Printf("failed to query lora count: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"maxPage": (count - 1) / browseLoraPageLimit})
	}
}

func apiBrowseLoraPage(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		var u struct {
			Page int `uri:"page"`
		}
		if err := ctx.BindUri(&u); err != nil {
			log.Printf("failed to bind uri parameters: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		loraIds, names, versions, err := queryLoraByPage(appCtx.dbr, u.Page)
		if err != nil {
			log.Printf("failed to query lora by page: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
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
		count, err := queryLoraCombinationCount(appCtx.dbr)
		if err != nil {
			log.Printf("failed to query lora combination count: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"maxPage": (count - 1) / browseCombinationLimit})
	}
}

func apiBrowseCombinationPage(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		var u struct {
			Page int `uri:"page"`
		}
		if err := ctx.BindUri(&u); err != nil {
			log.Printf("failed to bind uri parameters: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		combinationIds, err := queryLoraCombinationByPage(appCtx.dbr, u.Page)
		if err != nil {
			log.Printf("failed to query lora combinations by page: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		ctx.JSON(http.StatusOK, gin.H{
			"combinationIds": combinationIds,
		})
	}
}

func apiLoraLoraId(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		var u struct {
			LoraId int `uri:"loraId" binding:"required"`
		}
		if err := ctx.BindUri(&u); err != nil {
			log.Printf("failed to bind uri parameters: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		lora, err := queryLora(appCtx.dbr, u.LoraId)
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
		var u struct {
			LoraId int `uri:"loraId" binding:"required"`
		}
		if err := ctx.BindUri(&u); err != nil {
			log.Printf("failed to bind uri parameters: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		urlpreview, err := queryLoraPreview(appCtx.dbr, u.LoraId)
		if err != nil {
			log.Printf("failed to query lora preview: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if urlpreview == nil {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}

		ctx.Header("Cache-Control", "public, max-age=604800")
		ctx.Data(200, "image/png", urlpreview)
	}
}

func apiLoraLoraIdPreviewSampleImage(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		var u struct {
			LoraId     int `uri:"loraId" binding:"required"`
			SampleType int `uri:"sampleType" binding:"required"`
		}
		if err := ctx.BindUri(&u); err != nil {
			log.Printf("failed to bind uri parameters: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		image, err := queryLoraPreviewSampleImage(appCtx.dbr, u.LoraId, u.SampleType)
		if err != nil {
			log.Printf("failed to query lora preview sample image: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if image == nil {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}

		ctx.Header("Cache-Control", "public, max-age=604800")
		ctx.Data(200, "image/png", image)
	}
}

func apiLoraLoraIdSampleImageCheckpointFilenamePromtlistIdSampleType(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		var u struct {
			LoraId             int    `uri:"loraId" binding:"required"`
			CheckpointFilename string `uri:"checkpointFilename" binding:"required"`
			PromptListId       int    `uri:"promptlistId" binding:"required"`
			SampleType         int    `uri:"sampleType" binding:"required"`
		}
		if err := ctx.BindUri(&u); err != nil {
			log.Printf("failed to bind uri parameters: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		image, err := queryLoraSampleImage(appCtx.dbr, u.LoraId, u.PromptListId, u.CheckpointFilename, u.SampleType)
		if err != nil {
			log.Printf("failed to query lora sample image: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if image == nil {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}

		ctx.Header("Cache-Control", "public, max-age=604800")
		ctx.Data(200, "image/png", image)
	}
}

func apiCombinationCombinationIdSampleImageCheckpointFilenameSampleType(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		var u struct {
			CombinationId      int    `uri:"combinationId" binding:"required"`
			CheckpointFilename string `uri:"checkpointFilename" binding:"required"`
			SampleType         int    `uri:"sampleType" binding:"required"`
		}
		if err := ctx.BindUri(&u); err != nil {
			log.Printf("failed to bind uri parameters: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		image, err := queryCombinationSampleImage(appCtx.dbr, u.CombinationId, u.CheckpointFilename, u.SampleType)
		if err != nil {
			log.Printf("failed to query combination sample image: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if image == nil {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}

		ctx.Header("Cache-Control", "public, max-age=604800")
		ctx.Data(200, "image/png", image)
	}
}

func apiCombinationCombinationIdPreviewSampleType(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		var u struct {
			CombinationId int `uri:"combinationId" binding:"required"`
			SampleType    int `uri:"sampleType" binding:"required"`
		}
		if err := ctx.BindUri(&u); err != nil {
			log.Printf("failed to bind uri parameters: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		image, err := queryCombinationPreviewSampleImage(appCtx.dbr, u.CombinationId, u.SampleType)
		if err != nil {
			log.Printf("failed to query combination preview sample image: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if image == nil {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}

		ctx.Header("Cache-Control", "public, max-age=604800")
		ctx.Data(200, "image/png", image)
	}
}

func getSubmitLoraV2(appCtx AppContext) func(ctx *gin.Context) {
	return func(ctx *gin.Context) {
		ctx.Header("Content-Type", "text/html")
		ctx.String(http.StatusOK, appCtx.htmlTemplates.submitLoraV2Html)
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

func getApiV2CivitaiModelModelIdVersion(appCtx AppContext) func(ctx *gin.Context) {
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
		versionNames := make([]string, len(model.ModelVersions))
		versionIds := make([]int, len(model.ModelVersions))
		promptList := make([][]string, len(model.ModelVersions))
		for index, modelVersion := range model.ModelVersions {
			versionNames[index] = modelVersion.Name
			versionIds[index] = modelVersion.Id
			if modelVersion.TrainedWords == nil {
				modelVersion.TrainedWords = make([]string, 0)
			}
			promptList[index] = modelVersion.TrainedWords
		}
		ctx.JSON(http.StatusOK, gin.H{
			"versionNames": versionNames,
			"versionIds":   versionIds,
			"promptList":   promptList,
		})
	}
}
