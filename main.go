package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	textTemplate "text/template"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/spf13/viper"
)

type AppContextHtmlTemplates struct {
	loraHtml         *template.Template
	combinationHtml  *template.Template
	submitLoraV2Html string
}

type DB interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
}

type AppContext struct {
	db  *sql.DB
	dbr *sql.DB

	htmlTemplates AppContextHtmlTemplates
	Config        struct {
		ComfyuiEndpoint string `mapstructure:"COMFYUI_ENDPOINT"`
		ComfyuiLoraPath string `mapstructure:"COMFYUI_LORA_PATH"`
		CivitaiApiToken string `mapstructure:"CIVITAI_API_TOKEN"`
	}
}

func readFormFile(ctx *gin.Context, name string) ([]byte, error) {
	header, err := ctx.FormFile(name)
	if err != nil {
		return nil, fmt.Errorf("failed to read form file header: %w", err)
	}

	file, err := header.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open form file: %w", err)
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return fileBytes, nil
}

func main() {
	appCtx := AppContext{}

	viper.SetConfigFile(".env")
	viper.SetConfigType("env")
	viper.AddConfigPath(".")
	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("failed to read config file: %v\n", err)
	} else if err := viper.Unmarshal(&appCtx.Config); err != nil {
		log.Fatalf("failed to unmarshal config file: %v\n", err)
	}

	var err error
	appCtx.db, err = sql.Open("sqlite3", "./test.db?_busy_timeout=1000&_journal_mode=WAL&_foreign_keys=true")
	if err != nil {
		log.Fatalf("failed to open DB file: %v", err)
	}
	defer appCtx.db.Close()
	appCtx.db.SetMaxOpenConns(1)

	appCtx.dbr, err = sql.Open("sqlite3", "./test.db?_busy_timeout=1000&_journal_mode=WAL&mode=ro")
	if err != nil {
		log.Fatalf("failed to open DB file for readonly: %v", err)
	}
	defer appCtx.dbr.Close()

	if initDbTx, err := appCtx.db.Begin(); err != nil {
		log.Fatalf("failed to start transaction for DB init: %v\n", err)
	} else if err := initDB(initDbTx); err != nil {
		initDbTx.Rollback()
		log.Fatalf("failed to init DB: %v\n", err)
	} else if err := initDbTx.Commit(); err != nil {
		initDbTx.Rollback()
		log.Fatalf("failed to commit init DB transaction: %v\n", err)
	}

	if appCtx.htmlTemplates.loraHtml, err = template.New("lora").Parse(loraHtml); err != nil {
		log.Fatalf("failed to parse lora html template: %v\n", err)
	} else if appCtx.htmlTemplates.combinationHtml, err = template.New("lora").Parse(combinationHtml); err != nil {
		log.Fatalf("failed to parse lora html template: %v\n", err)
	}

	if submitLoraV2Html, err := textTemplate.New("submit lora v2").Parse(submitLoraV2Html); err != nil {
		log.Fatalf("failed to parse submit lora v2 html template: %v\n", err)
	} else {
		var buffer bytes.Buffer
		if err := submitLoraV2Html.Execute(&buffer, struct {
			SubmitLoraV2Script0 string
		}{
			SubmitLoraV2Script0: submitLoraV2Js,
		}); err != nil {
			log.Fatalf("failed to execute submit lora v2 html template: %v\n", err)
		}
		appCtx.htmlTemplates.submitLoraV2Html = buffer.String()
	}

	router := gin.Default()
	initGetRouters(router, appCtx)
	initPutRouters(router, appCtx)
	initPostRouters(router, appCtx)
	initDeleteRouters(router, appCtx)

	server := &http.Server{
		Addr:    "192.168.123.10:8080",
		Handler: router.Handler(),
	}

	chStopped := make(chan struct{})
	chStop := make(chan struct{})
	go downloadModelRoutine(appCtx, chModelDownloadQueue, chStop, chStopped)

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("error returned while listening and serving HTTP server: %v", err)
		}
	}()

	chInterruptNotify := make(chan os.Signal, 1)
	signal.Notify(chInterruptNotify, syscall.SIGINT, syscall.SIGTERM)
	<-chInterruptNotify

	ctx := context.Background()
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("error returned while waiting for HTTP server shutdown: %v", err)
	}

	close(chStop)
	<-chStopped
}
