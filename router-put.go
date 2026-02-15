package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func initPutRouters(engine *gin.Engine, appCtx AppContext) {
	for path, router := range putMappings {
		engine.PUT(path, router(appCtx))
	}
}

var putMappings = map[string]func(AppContext) func(*gin.Context){
	"/api/lora/:loraId/sampleImage/:checkpointFilename/:promptlistId/:sampleType":           putApiLoraLoraIdSampleImageCheckpointFilenamePromptListIdSampleType,
	"/api/combination/:loraCombinationId/sampleImage/:checkpointFilename/:sampleType":       putApiCombinationLoraCombinationIdSampleImageCheckpointFilenameSampleType,
	"/api/combination-test/:testId/sampleImage/:checkpointFilename/:trialIndex/:sampleType": putApiCombinationTestTestIdSampleImageCheckpointFilenameTrialIndexSampleType,
}

func putApiLoraLoraIdSampleImageCheckpointFilenamePromptListIdSampleType(appCtx AppContext) func(*gin.Context) {
	return func(ctx *gin.Context) {
		var u struct {
			LoraId             int    `uri:"loraId" binding:"required"`
			PromptListId       int    `uri:"promptlistId" binding:"required"`
			CheckpointFilename string `uri:"checkpointFilename" binding:"required"`
			SampleType         int    `uri:"sampleType" binding:"required"`
		}
		if err := ctx.BindUri(&u); err != nil {
			log.Printf("failed to bind uri parameters: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		sampleImage, err := readFormFile(ctx, "sampleimage")
		if err != nil {
			log.Printf("failed to read form file: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		if err := insertSampleImage(appCtx.db, u.LoraId, u.PromptListId, u.CheckpointFilename, u.SampleType, sampleImage); err != nil {
			log.Printf("failed to insert new sample image row: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		ctx.Status(http.StatusOK)
	}
}

func putApiCombinationLoraCombinationIdSampleImageCheckpointFilenameSampleType(appCtx AppContext) func(*gin.Context) {
	return func(ctx *gin.Context) {
		var u struct {
			LoraCombinationId  int    `uri:"loraCombinationId" binding:"required"`
			CheckpointFilename string `uri:"checkpointFilename" binding:"required"`
			SampleType         int    `uri:"sampleType" binding:"required"`
		}

		sampleImage, err := readFormFile(ctx, "sampleimage")
		if err != nil {
			log.Printf("failed to read form file: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		stmt := `INSERT INTO loraCombinationSampleImages ( loraCombinationId, checkpointFilename, sampleType, sampleImage ) VALUES ( ?, ?, ?, ? )`

		if _, err := appCtx.db.Exec(stmt, u.LoraCombinationId, u.CheckpointFilename, u.SampleType, sampleImage); err != nil {
			log.Printf("failed to insert new sample image row: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		ctx.Status(http.StatusOK)
	}
}

func putApiCombinationTestTestIdSampleImageCheckpointFilenameTrialIndexSampleType(appCtx AppContext) func(*gin.Context) {
	return func(ctx *gin.Context) {
		var u struct {
			TestId             int    `uri:"testId" binding:"required"`
			CheckpointFilename string `uri:"checkpointFilename" binding:"required"`
			TrialIndex         int    `uri:"trialIndex" binding:"required"`
			SampleType         int    `uri:"sampleType" binding:"required"`
		}
		if err := ctx.BindUri(&u); err != nil {
			log.Printf("failed to bind uri parameter: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		sampleImage, err := readFormFile(ctx, "sampleimage")
		if err != nil {
			log.Printf("failed to read form file: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		stmt := `INSERT INTO loraCombinationTestTrialSampleImages ( loraCombinationTestId, trialIndex, checkpointFilename, sampleType, sampleImage ) VALUES ( ?, ?, ?, ?, ? )`
		if _, err := appCtx.db.Exec(stmt, u.TestId, u.TrialIndex, u.CheckpointFilename, u.SampleType, sampleImage); err != nil {
			log.Printf("failed to insert new sample image row: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		ctx.Status(http.StatusOK)
	}
}

func putApiCheckpointFilename(appCtx AppContext) func(*gin.Context) {
	return func(ctx *gin.Context) {
		var u struct {
			Filename string `uri:"filename" binding:"required"`
		}
		var q struct {
			Name    string `form:"name" binding:"required"`
			Version string `form:"version" binding:"required"`
		}
		if err := ctx.BindUri(&u); err != nil {
			log.Printf("failed to bind uri parameters: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if err := ctx.BindQuery(&q); err != nil {
			log.Printf("failed to bind query parameters: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		if err := insertCheckpoint(appCtx.db, u.Filename, q.Name, q.Version); err != nil {
			log.Printf("failed to insert checkpoint row: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		ctx.Status(http.StatusOK)
	}
}
