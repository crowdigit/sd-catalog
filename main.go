package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

var comfyUiEndpoint = "192.168.123.10:8081"
var comfyUiLoraPath = "C:\\Users\\asdf\\Tools\\ComfyUI_windows_portable\\ComfyUI\\models\\loras\\styles - artist"

// var comfyUiLoraPath = "C:\\Users\\asdf\\Tools\\ComfyUI_windows_portable\\ComfyUI\\models\\loras\\testing"

type PostLoraForm struct {
	Title    string `form:"title"`
	URL      string `form:"url"`
	Version  string `form:"version"`
	Filename string `form:"filename"`
	Prompts  string `form:"prompts"`
}

func (f PostLoraForm) Coalesce(urlpreview []byte) (Lora, error) {
	prompts := make([][]string, 0)
	if err := json.Unmarshal([]byte(f.Prompts), &prompts); err != nil {
		return Lora{}, fmt.Errorf("failed to unmarshal prompts JSON string into array: %v", err)
	}
	return Lora{
		Name:        f.Title,
		Url:         f.URL,
		UrlPreview:  urlpreview,
		Version:     f.Version,
		Filename:    f.Filename,
		PromptLists: prompts,
		Tags:        nil,
	}, nil
}

type AppContextHtmlTemplates struct {
	loraHtml        *template.Template
	combinationHtml *template.Template
}

type AppContext struct {
	db            *sql.DB
	htmlTemplates AppContextHtmlTemplates
}

func main() {
	db, err := sql.Open("sqlite3", "./test.db?_busy_timeout=1000&_journal_mode=WAL&_foreign_keys=true")
	if err != nil {
		log.Fatalf("failed to open DB file: %v", err)
	}
	defer db.Close()

	if err := initDB(db); err != nil {
		log.Fatalf("failed to create DB: %v", err)
	}

	appCtx := AppContext{
		db:            db,
		htmlTemplates: AppContextHtmlTemplates{},
	}

	if appCtx.htmlTemplates.loraHtml, err = template.New("lora").Parse(loraHtml); err != nil {
		log.Fatalf("failed to parse lora html template: %v\n", err)
	} else if appCtx.htmlTemplates.combinationHtml, err = template.New("lora").Parse(combinationHtml); err != nil {
		log.Fatalf("failed to parse lora html template: %v\n", err)
	}

	router := gin.Default()
	initGetRouters(router, appCtx)

	router.POST("/api/lora", func(ctx *gin.Context) {
		var postLoraForm PostLoraForm
		if err := ctx.Bind(&postLoraForm); err != nil {
			log.Printf("failed to bind post lora form: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		urlpreviewHeader, err := ctx.FormFile("urlpreview")
		if err != nil {
			log.Printf("failed to get urlpreview file from post lora form: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		urlpreviewFile, err := urlpreviewHeader.Open()
		if err != nil {
			log.Printf("failed to open urlpreview file from post lora form: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		defer urlpreviewFile.Close()
		urlpreview, err := io.ReadAll(urlpreviewFile)
		if err != nil {
			log.Printf("failed to read urlpreview file: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		lora, err := postLoraForm.Coalesce(urlpreview)
		if err != nil {
			log.Printf("failed to coalesce post lora form: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		loraId, err := insertLoraRow(db, lora)
		if err != nil {
			log.Printf("failed to insert new lora row: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		u := url.URL{
			Scheme: "http",
			Host:   comfyUiEndpoint,
			Path:   "/api/queue",
		}
		rows, err := db.Query("SELECT checkpointFilename FROM defaultCheckpoint LIMIT 1")
		if err != nil {
			log.Printf("failed to query default checkpoint filename: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		if !rows.Next() {
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
	})

	router.POST("/api/combination", func(ctx *gin.Context) {
		var postCombinationForm struct {
			Ids       string `form:"ids"`
			Strengths string `form:"strengths"`
			Prompts   string `form:"prompts"`
		}
		if err := ctx.Bind(&postCombinationForm); err != nil {
			log.Printf("failed to bind post combination form: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		var postCombinationFormCoalesced struct {
			Ids       []int
			Strengths []int
			Prompts   []string
		}
		if err := json.Unmarshal([]byte(postCombinationForm.Ids), &postCombinationFormCoalesced.Ids); err != nil {
			log.Printf("failed to unmarshal ids into integer array: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if err := json.Unmarshal([]byte(postCombinationForm.Strengths), &postCombinationFormCoalesced.Strengths); err != nil {
			log.Printf("failed to unmarshal strengths into integer array: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if err := json.Unmarshal([]byte(postCombinationForm.Prompts), &postCombinationFormCoalesced.Prompts); err != nil {
			log.Printf("failed to unmarshal prompts into string array: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		abort := false
		tx, err := db.Begin()
		if err != nil {
			log.Printf("failed to start lora combination transaction: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			abort = true
			return
		}
		defer func() {
			if abort {
				tx.Rollback()
			}
		}()

		stmt0 := `INSERT INTO loraCombinations DEFAULT VALUES`
		stmt0Result, err := tx.Exec(stmt0)
		if err != nil {
			log.Printf("failed to insert new lora combination: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			abort = true
			return
		}
		loraCombinationId, err := stmt0Result.LastInsertId()
		if err != nil {
			log.Printf("failed to get new lora combination ID: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			abort = true
			return
		}
		for index, loraId := range postCombinationFormCoalesced.Ids {
			stmt1 := `INSERT INTO loraCombinationComponents ( loraCombinationId, loraId, seq, strength ) VALUES ( ?, ?, ?, ? )`
			if _, err := tx.Exec(stmt1, loraCombinationId, loraId, index+1, postCombinationFormCoalesced.Strengths[index]); err != nil {
				log.Printf("failed to insert lora combination component: %v\n", err)
				ctx.AbortWithStatus(http.StatusBadRequest)
				abort = true
				return
			}
		}

		for index, prompt := range postCombinationFormCoalesced.Prompts {
			stmt2 := `INSERT INTO loraCombinationPrompts ( loraCombinationId, seq, prompt ) VALUES ( ?, ?, ? )`
			if _, err := tx.Exec(stmt2, loraCombinationId, index+1, prompt); err != nil {
				log.Printf("failed to insert lora combination prompt: %v\n", err)
				ctx.AbortWithStatus(http.StatusBadRequest)
				abort = true
				return
			}
		}

		if err := tx.Commit(); err != nil {
			log.Printf("failed to commit lora combination transaction: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			abort = true
			return
		}

		u := url.URL{
			Scheme: "http",
			Host:   comfyUiEndpoint,
			Path:   "/api/queue-combination",
		}
		rows, err := db.Query("SELECT checkpointFilename FROM defaultCheckpoint LIMIT 1")
		if err != nil {
			log.Printf("failed to query default checkpoint filename: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		if !rows.Next() {
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
		q := u.Query()
		q.Set("combinationId", strconv.Itoa(int(loraCombinationId)))
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
	})

	router.POST("/api/combination-test", func(ctx *gin.Context) {
		f := struct {
			Ids     string `form:"ids"`
			Prompts string `form:"prompts"`
		}{}
		if err := ctx.Bind(&f); err != nil {
			log.Printf("failed to bind post combination test form: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		var fc struct {
			Ids     []int
			Prompts []string
		}
		if err := json.Unmarshal([]byte(f.Ids), &fc.Ids); err != nil {
			log.Printf("failed to unmarshal ids into integer array: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if err := json.Unmarshal([]byte(f.Prompts), &fc.Prompts); err != nil {
			log.Printf("failed to unmarshal prompts into string array: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		abort := false
		tx, err := db.Begin()
		if err != nil {
			log.Printf("failed to start lora combination transaction: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			abort = true
			return
		}
		defer func() {
			if abort {
				tx.Rollback()
			}
		}()

		stmt0 := `INSERT INTO loraCombinationTests DEFAULT VALUES`
		stmt0Result, err := tx.Exec(stmt0)
		if err != nil {
			log.Printf("failed to insert new lora combination test: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			abort = true
			return
		}
		testId, err := stmt0Result.LastInsertId()
		if err != nil {
			log.Printf("failed to get new lora combination test ID: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			abort = true
			return
		}
		for index, loraId := range fc.Ids {
			stmt1 := `INSERT INTO loraCombinationTestComponents ( loraCombinationTestId, componentSeq, loraId ) VALUES ( ?, ?, ? )`
			if _, err := tx.Exec(stmt1, testId, index+1, loraId); err != nil {
				log.Printf("failed to insert lora combination test component: %v\n", err)
				ctx.AbortWithStatus(http.StatusBadRequest)
				abort = true
				return
			}
		}

		for index, prompt := range fc.Prompts {
			stmt2 := `INSERT INTO loraCombinationTestPrompts ( loraCombinationTestId, promptSeq, prompt ) VALUES ( ?, ?, ? )`
			if _, err := tx.Exec(stmt2, testId, index+1, prompt); err != nil {
				log.Printf("failed to insert lora combination test prompt: %v\n", err)
				ctx.AbortWithStatus(http.StatusBadRequest)
				abort = true
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
		currentEnum := make([]int, len(fc.Ids))
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
					if i == len(fc.Ids)-1 {
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
			abort = true
			return
		}

		u := url.URL{
			Scheme: "http",
			Host:   comfyUiEndpoint,
			Path:   "/api/queue-combination-test",
		}
		rows, err := db.Query("SELECT checkpointFilename FROM defaultCheckpoint LIMIT 1")
		if err != nil {
			log.Printf("failed to query default checkpoint filename: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		if !rows.Next() {
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
		q := u.Query()
		q.Set("testId", strconv.Itoa(int(testId)))
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
	})

	router.PUT("/api/lora/:loraId/sampleImage/:checkpointFilename/:promptlistId/:sampleType", func(ctx *gin.Context) {
		loraId := ctx.Param("loraId")
		promptlistId := ctx.Param("promptlistId")
		checkpointFilename := ctx.Param("checkpointFilename")
		sampleType, err := strconv.Atoi(ctx.Param("sampleType"))
		if err != nil {
			log.Printf("failed to convert sample type parameter into integer: %v\n", err)
		}
		sampleImageHeader, err := ctx.FormFile("sampleimage")
		if err != nil {
			log.Printf("failed to get sampleimage file from post sample image form: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		sampleImageFile, err := sampleImageHeader.Open()
		if err != nil {
			log.Printf("failed to open sampleimage file: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		defer sampleImageFile.Close()
		sampleImage, err := io.ReadAll(sampleImageFile)
		if err != nil {
			log.Printf("failed to read sampleimage file: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		if err := insertSampleImage(db, loraId, promptlistId, checkpointFilename, sampleType, sampleImage); err != nil {
			log.Printf("failed to insert new sample image row: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		ctx.Status(http.StatusOK)
	})

	router.PUT("/api/combination/:loraCombinationId/sampleImage/:checkpointFilename/:sampleType", func(ctx *gin.Context) {
		loraCombinationId, err := strconv.Atoi(ctx.Param("loraCombinationId"))
		if err != nil {
			log.Printf("failed to convert lora combination id parameter into integer: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		checkpointFilename := ctx.Param("checkpointFilename")
		sampleType, err := strconv.Atoi(ctx.Param("sampleType"))
		if err != nil {
			log.Printf("failed to convert sample type parameter into integer: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		sampleImageHeader, err := ctx.FormFile("sampleimage")
		if err != nil {
			log.Printf("failed to get sampleimage file from post sample image form: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		sampleImageFile, err := sampleImageHeader.Open()
		if err != nil {
			log.Printf("failed to open sampleimage file: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		defer sampleImageFile.Close()
		sampleImage, err := io.ReadAll(sampleImageFile)
		if err != nil {
			log.Printf("failed to read sampleimage file: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		stmt := `INSERT INTO loraCombinationSampleImages ( loraCombinationId, checkpointFilename, sampleType, sampleImage ) VALUES ( ?, ?, ?, ? )`

		if _, err := db.Exec(stmt, loraCombinationId, checkpointFilename, sampleType, sampleImage); err != nil {
			log.Printf("failed to insert new sample image row: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		ctx.Status(http.StatusOK)
	})

	router.PUT("/api/combination-test/:testId/sampleImage/:checkpointFilename/:trialIndex/:sampleType", func(ctx *gin.Context) {
		u := struct {
			TestId             int    `uri:"testId" binding:"required"`
			CheckpointFilename string `uri:"checkpointFilename" binding:"required"`
			TrialIndex         int    `uri:"trialIndex" binding:"required"`
			SampleType         int    `uri:"sampleType" binding:"required"`
		}{}
		if err := ctx.BindUri(&u); err != nil {
			log.Printf("failed to bind uri parameter: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		f := struct {
			SampleImage *multipart.FileHeader `form:"sampleimage" binding:"required"`
		}{}

		if err := ctx.Bind(&f); err != nil {
			log.Printf("failed to bind request data: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		sampleImageFile, err := f.SampleImage.Open()
		if err != nil {
			log.Printf("failed to open sampleimage file: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		defer sampleImageFile.Close()
		sampleImage, err := io.ReadAll(sampleImageFile)
		if err != nil {
			log.Printf("failed to read sampleimage file: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		stmt := `INSERT INTO loraCombinationTestTrialSampleImages ( loraCombinationTestId, trialIndex, checkpointFilename, sampleType, sampleImage ) VALUES ( ?, ?, ?, ?, ? )`
		if _, err := db.Exec(stmt, u.TestId, u.TrialIndex, u.CheckpointFilename, u.SampleType, sampleImage); err != nil {
			log.Printf("failed to insert new sample image row: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		ctx.Status(http.StatusOK)
	})

	router.PUT("/api/checkpoint/:filename", func(ctx *gin.Context) {
		filename := ctx.Param("filename")
		name := ctx.Query("name")
		version := ctx.Query("version")
		log.Printf("%s %s %s\n", filename, name, version)
		if err := insertCheckpoint(db, filename, name, version); err != nil {
			log.Printf("failed to insert checkpoint row: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		ctx.Status(http.StatusOK)
	})

	router.DELETE("/api/lora/:loraId", func(ctx *gin.Context) {
		loraId, err := strconv.Atoi(ctx.Param("loraId"))
		if err != nil {
			log.Printf("failed to parse lora ID into integer: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		lora, err := queryLora(db, loraId)
		if err != nil {
			log.Printf("failed to query lora: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		} else if lora == nil {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}
		if rows, err := db.Exec("DELETE FROM loras WHERE loraId = ?", loraId); err != nil {
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
		if err := os.Remove(filepath.Join(comfyUiLoraPath, lora.Filename)); err != nil {
			log.Printf("failed to delete lora file: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		ctx.Status(http.StatusOK)
	})

	server := &http.Server{
		Addr:    "192.168.123.10:8080",
		Handler: router.Handler(),
	}

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
}
