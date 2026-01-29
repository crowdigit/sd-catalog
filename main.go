package main

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

var comfyUiEndpoint = "192.168.123.10:8081"
var defaultCheckpoint = "waiSHUFFLENOOB_vPred04.safetensors"

type Lora struct {
	Name        string
	Url         string
	UrlPreview  []byte
	Version     string
	Filename    string
	PromptLists [][]string
	Tags        []string
}

func insertLoraRow(db *sql.DB, lora Lora) (int64, error) {
	stmt := `INSERT INTO loras ( name, url, urlpreview, version, filename ) VALUES ( ?, ?, ?, ?, ? )`
	result, err := db.Exec(stmt, lora.Name, lora.Url, lora.UrlPreview, lora.Version, lora.Filename)
	if err != nil {
		return 0, fmt.Errorf("failed to execute insert lora statement: %w", err)
	}

	loraId, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get inserted lora row id: %w", err)
	}

	promptListStmt := `INSERT INTO promptLists ( loraId, promptListId ) VALUES ( ?, ? )`
	promptStmt := `INSERT INTO prompts ( loraId, promptListId, seq, prompt ) VALUES ( ?, ?, ?, ? )`
	for promptListIndex, promptList := range lora.PromptLists {
		if _, err := db.Exec(promptListStmt, loraId, promptListIndex+1); err != nil {
			return 0, fmt.Errorf("failed to execute insert prompt list statement: %w", err)
		}
		for promptIndex, prompt := range promptList {
			if _, err := db.Exec(promptStmt, loraId, promptListIndex+1, promptIndex+1, prompt); err != nil {
				return 0, fmt.Errorf("failed to execute insert prompt statement: %w", err)
			}
		}
	}

	tagsStmt := `INSERT INTO tags ( loraId, tag ) VALUES ( ?, ? )`
	for _, tag := range lora.Tags {
		if _, err := db.Exec(tagsStmt, loraId, tag); err != nil {
			return 0, fmt.Errorf("failed to execute insert tag statement: %w", err)
		}
	}

	return loraId, nil
}

func insertSampleImage(db *sql.DB, loraId string, promptlistId string, checkpointFilename string, sampleType int, sampleImage []byte) error {
	stmt := `INSERT INTO sampleImages ( loraId, promptlistId, checkpointFilename, sampleType, sampleImage ) VALUES ( ?, ?, ?, ?, ? )`
	_, err := db.Exec(stmt, loraId, promptlistId, checkpointFilename, sampleType, sampleImage)
	if err != nil {
		return fmt.Errorf("failed to execute insert sample image statement: %w", err)
	}
	return nil
}

func insertCheckpoint(db *sql.DB, checkpointFilename string, name string, version string) error {
	stmt := `INSERT INTO checkpoints ( checkpointFilename, name, version ) VALUES ( ?, ?, ? )`
	_, err := db.Exec(stmt, checkpointFilename, name, version)
	if err != nil {
		return fmt.Errorf("failed to insert checkpoint row: %w", err)
	}
	return nil
}

func initDB(db *sql.DB) error {
	stmt1 := `CREATE TABLE IF NOT EXISTS
loras (
    loraId INTEGER PRIMARY KEY ASC AUTOINCREMENT,
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    urlpreview BLOB NOT NULL,
    version TEXT NOT NULL,
    filename TEXT NOT NULL,
    UNIQUE ( url, version ) ON CONFLICT FAIL,
    UNIQUE ( filename ) ON CONFLICT FAIL,
    CHECK ( version <> "" AND filename <> "")
)`
	if _, err := db.Exec(stmt1); err != nil {
		return fmt.Errorf("failed to execute create loras table statement: %w", err)
	}

	stmt2 := `CREATE TABLE IF NOT EXISTS
promptLists (
    loraId REFERENCES loras ( loraId ) ON DELETE CASCADE,
    promptListId INTEGER NOT NULL,
    UNIQUE ( loraId, promptListId ) ON CONFLICT FAIL
)`
	if _, err := db.Exec(stmt2); err != nil {
		return fmt.Errorf("failed to execute create prompt lists table statement: %w", err)
	}

	stmt3 := `CREATE TABLE IF NOT EXISTS
prompts (
    loraId INTEGER NOT NULL,
    promptListId INTEGER NOT NULL,
    seq INTEGER NOT NULL,
    prompt TEXT NOT NULL,
    FOREIGN KEY ( loraId, promptListId ) REFERENCES promptLists ( loraId, promptListId ) ON DELETE CASCADE,
    UNIQUE ( loraId, promptListId, seq ) ON CONFLICT FAIL,
    CHECK ( prompt <> "")
)`
	if _, err := db.Exec(stmt3); err != nil {
		return fmt.Errorf("failed to execute create prompts table statement: %w", err)
	}

	stmt4 := `CREATE TABLE IF NOT EXISTS
tags (
    loraId INTEGER REFERENCES loras ( loraId ) ON DELETE CASCADE,
    tag TEXT NOT NULL,
    UNIQUE ( loraId, tag ) ON CONFLICT IGNORE,
    CHECK ( tag <> "")
)`
	if _, err := db.Exec(stmt4); err != nil {
		return fmt.Errorf("failed to execute create tags table statement: %w", err)
	}

	stmt5 := `CREATE TABLE IF NOT EXISTS
checkpoints (
    checkpointFilename TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    CHECK ( checkpointFilename <> "" AND name <> "" AND version <> "")
)`
	if _, err := db.Exec(stmt5); err != nil {
		return fmt.Errorf("failed to create checkpoint table: %w", err)
	}

	stmt6 := `CREATE TABLE IF NOT EXISTS
sampleImages (
    loraId INTEGER NOT NULL,
    promptListId INTEGER NOT NULL,
    checkpointFilename TEXT NOT NULL,
    sampleType INTEGER NOT NULL,
    sampleImage BLOB NOT NULL,
    FOREIGN KEY ( loraId, promptListId ) REFERENCES promptLists ( loraId, promptListId ) ON DELETE CASCADE,
    FOREIGN KEY ( checkpointFilename ) REFERENCES checkpoints ( checkpointFilename ) ON DELETE CASCADE,
    UNIQUE ( loraId, promptListId, checkpointFilename, sampleType ) ON CONFLICT REPLACE
)`
	if _, err := db.Exec(stmt6); err != nil {
		return fmt.Errorf("failed to execute create sample images table statement: %w", err)
	}
	return nil
}

//go:embed submit-lora.html
var submitLoraHtml string

//go:embed submit-checkpoint.html
var submitCheckpointHtml string

//go:embed index.html
var indexHtml string

//go:embed browse-lora.html
var browseLoraHtml string

//go:embed lora.html
var loraHtml string

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

func main() {
	db, err := sql.Open("sqlite3", "./test.db")
	if err != nil {
		log.Fatalf("failed to open DB file: %v", err)
	}
	defer db.Close()

	if err := initDB(db); err != nil {
		log.Fatalf("failed to create DB: %v", err)
	}

	router := gin.Default()

	router.GET("/", func(ctx *gin.Context) {
		ctx.Header("Content-Type", "text/html")
		ctx.String(http.StatusOK, indexHtml)
	})

	router.GET("/submit-lora", func(ctx *gin.Context) {
		ctx.Header("Content-Type", "text/html")
		ctx.String(http.StatusOK, submitLoraHtml)
	})

	router.GET("/submit-checkpoint", func(ctx *gin.Context) {
		ctx.Header("Content-Type", "text/html")
		ctx.String(http.StatusOK, submitCheckpointHtml)
	})

	router.GET("/browse-lora", func(ctx *gin.Context) {
		ctx.Header("Content-Type", "text/html")
		ctx.String(http.StatusOK, browseLoraHtml)
	})

	router.GET("/lora/:loraId", func(ctx *gin.Context) {
		ctx.Header("Content-Type", "text/html")
		ctx.String(http.StatusOK, loraHtml)
	})

	limit := 5

	router.GET("/api/browse", func(ctx *gin.Context) {
		rows, err := db.Query("SELECT COUNT(*) AS num FROM loras")
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
		response := struct {
			MaxPage int `json:"maxPage"`
		}{
			MaxPage: (num - 1) / limit,
		}
		ctx.JSON(http.StatusOK, response)
	})
	router.GET("/api/browse/:page", func(ctx *gin.Context) {
		page, err := strconv.Atoi(ctx.Param("page"))
		if err != nil {
			log.Printf("failed to parse page into integer: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		offset := limit * page
		rows, err := db.Query("SELECT loraId, name, version FROM loras ORDER BY loraId ASC LIMIT ? OFFSET ?", limit, offset)
		if err != nil {
			log.Printf("failed to select lora IDs: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}
		loraIds := make([]int, 0, limit)
		names := make([]string, 0, limit)
		versions := make([]string, 0, limit)
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
		ctx.JSON(http.StatusOK, struct {
			LoraIds  []int    `json:"loraIds"`
			Names    []string `json:"names"`
			Versions []string `json:"versions"`
		}{
			LoraIds:  loraIds,
			Names:    names,
			Versions: versions,
		})
	})

	router.GET("/api/lora/:loraId", func(ctx *gin.Context) {
		loraId, err := strconv.Atoi(ctx.Param("loraId"))
		if err != nil {
			log.Printf("failed to parse lora ID into integer: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		rows, err := db.Query("SELECT name, url, version, filename FROM loras WHERE loraId = ?", loraId)
		if err != nil {
			log.Printf("failed to select lora: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		if !rows.Next() {
			ctx.AbortWithStatus(http.StatusNotFound)
			return
		}

		lora := struct {
			Name     string
			Url      string
			Version  string
			Filename string
		}{}
		if err := rows.Scan(&lora.Name, &lora.Url, &lora.Version, &lora.Filename); err != nil {
			ctx.AbortWithStatus(http.StatusBadRequest)
			log.Printf("failed to scan lora from query result: %v\n", err)
			return
		}

		ctx.Header("Cache-Control", "public, max-age=604800")
		ctx.JSON(http.StatusOK, lora)
	})

	router.GET("/api/lora/:loraId/preview", func(ctx *gin.Context) {
		loraId, err := strconv.Atoi(ctx.Param("loraId"))
		if err != nil {
			log.Printf("failed to parse lora ID into integer: %v\n", err)
			ctx.AbortWithStatus(http.StatusBadRequest)
			return
		}

		rows, err := db.Query("SELECT urlpreview FROM loras WHERE loraId = ?", loraId)
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
			log.Printf("failed to scan lora from query result: %v\n", err)
			return
		}

		ctx.Header("Cache-Control", "public, max-age=604800")
		ctx.Data(200, "image/png", urlpreview)
	})

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

	router.PUT("/api/lora/:loraId/sampleImage/:promptlistId/:checkpointFilename/:sampleType", func(ctx *gin.Context) {
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
