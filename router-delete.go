package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

var deleteMappings = map[string]func(AppContext) func(*gin.Context){
	"/api/lora/:loraId":    deleteApiLoraLoraId,
	"/api/v2/lora/:loraId": deleteApiV2LoraLoraId,
}

func initDeleteRouters(engine *gin.Engine, appCtx AppContext) {
	for path, router := range deleteMappings {
		engine.DELETE(path, router(appCtx))
	}
}

func deleteApiLoraLoraId(appCtx AppContext) func(*gin.Context) {
	return func(ctx *gin.Context) {
		var u struct {
			LoraId int `uri:"loraId" binding:"required"`
		}
		if err := ctx.BindUri(&u); err != nil {
			log.Printf("failed to bind uri parameters: %v\n", err)
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

		lora, err := queryLora(tx, u.LoraId)
		if err != nil {
			log.Printf("failed to query lora: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if lora == nil {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}
		if rows, err := tx.Exec("DELETE FROM loras WHERE loraId = ?", u.LoraId); err != nil {
			log.Printf("failed to delete lora: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if affected, err := rows.RowsAffected(); err != nil {
			log.Printf("failed to get affected row number: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if affected == 0 {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}
		if err := tx.Commit(); err != nil {
			log.Printf("failed to commit transaction: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		abort = false

		if err := os.Remove(filepath.Join(appCtx.Config.ComfyuiLoraPath, lora.Filename)); err != nil {
			log.Printf("failed to delete lora file: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		ctx.Status(http.StatusOK)
	}
}

func deleteApiV2LoraLoraId(appCtx AppContext) func(*gin.Context) {
	return func(ctx *gin.Context) {
		var u struct {
			LoraId int `uri:"loraId" binding:"required"`
		}
		if err := ctx.BindUri(&u); err != nil {
			log.Printf("failed to bind uri parameters: %v\n", err)
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

		lora, err := queryLoraV2(tx, u.LoraId)
		if err != nil {
			log.Printf("failed to query lora: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if lora == nil {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}
		if rows, err := tx.Exec("DELETE FROM lorasV2 WHERE loraId = ?", u.LoraId); err != nil {
			log.Printf("failed to delete lora: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if affected, err := rows.RowsAffected(); err != nil {
			log.Printf("failed to get affected row number: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if affected == 0 {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}
		if err := tx.Commit(); err != nil {
			log.Printf("failed to commit transaction: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		abort = false

		if err := os.Remove(filepath.Join(appCtx.Config.ComfyuiLoraPath, lora.Filename)); err != nil {
			log.Printf("failed to delete lora file: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		ctx.Status(http.StatusOK)
	}
}
