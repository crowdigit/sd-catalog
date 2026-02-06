package main

import (
	"database/sql"
	"fmt"
)

type LoraRow struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Version  string `json:"version"`
	Filename string `json:"filename"`
}

type PromptListItem struct {
	PromptListId int
	Prompts      []string
}

type Lora struct {
	Name        string
	Url         string
	UrlPreview  []byte
	Version     string
	Filename    string
	PromptLists [][]string
	Tags        []string
}

type LoraCombinationsComponent struct {
	LoraId   int
	Filename string
	Seq      int
	Strength int
	Name     string
	Version  string
}

type CheckpointRow struct {
	CheckpointFilename string
	Name               string
	Version            string
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

func queryDefaultCheckpoint(db *sql.DB) (string, error) {
	rows, err := db.Query("SELECT checkpointFilename FROM defaultCheckpoint LIMIT 1")
	if err != nil {
		return "", fmt.Errorf("failed to select from defaultCheckpoint: %w", err)
	}
	var defaultCheckpoint string
	if !rows.Next() {
		return "", fmt.Errorf("failed to scan default checkpoint: %w", err)
	} else if err := rows.Scan(&defaultCheckpoint); err != nil {
		return "", fmt.Errorf("failed to scan default checkpoint: %w", err)
	}
	return defaultCheckpoint, nil
}

func queryDefaultPromptListId(db *sql.DB, loraId int) (int, error) {
	rows, err := db.Query("SELECT promptListId FROM promptLists ORDER BY promptListId ASC LIMIT 1")
	if err != nil {
		return 0, fmt.Errorf("failed to select from promptLists: %w", err)
	}
	var defaultPromptListId int
	if !rows.Next() {
		return 0, fmt.Errorf("failed to scan default prompt list ID: %w", err)
	} else if rows.Scan(&defaultPromptListId); err != nil {
		return 0, fmt.Errorf("failed to scan default prompt list ID: %w", err)
	}
	return defaultPromptListId, nil
}

func queryLora(db *sql.DB, loraId int) (*LoraRow, error) {
	rows, err := db.Query("SELECT name, url, version, filename FROM loras WHERE loraId = ?", loraId)
	if err != nil {
		return nil, fmt.Errorf("failed to select lora: %w", err)
	}
	var lora LoraRow
	if !rows.Next() {
		return nil, nil
	} else if err := rows.Scan(&lora.Name, &lora.URL, &lora.Version, &lora.Filename); err != nil {
		return nil, fmt.Errorf("failed to scan lora: %v\n", err)
	}
	return &lora, nil
}

func queryLoraPrompts(db *sql.DB, loraId int) ([]PromptListItem, error) {
	stmt := `SELECT promptLists.promptListId, prompt
FROM promptLists
LEFT JOIN prompts
ON promptLists.loraId = prompts.loraId AND promptLists.promptListId = prompts.promptListId
WHERE promptLists.loraId = ?
ORDER BY promptLists.promptListId ASC, prompts.seq ASC`
	rows, err := db.Query(stmt, loraId)
	if err != nil {
		return nil, fmt.Errorf("failed to query prompts: %w", err)
	}

	promptList := make([]PromptListItem, 0, 1)
	for rows.Next() {
		var id int
		var prompt sql.NullString
		promptListLen := len(promptList)

		if err := rows.Scan(&id, &prompt); err != nil {
			return nil, fmt.Errorf("failed to scan prompt: %v\n", err)
		}

		var promptListItem *PromptListItem
		if promptListLen == 0 || promptList[promptListLen-1].PromptListId != id {
			promptList = append(promptList, PromptListItem{
				PromptListId: id,
				Prompts:      nil,
			})
			promptListItem = &promptList[promptListLen]
		} else {
			promptListItem = &promptList[promptListLen-1]
		}

		if prompt.Valid {
			promptListItem.Prompts = append(promptListItem.Prompts, prompt.String)
		}
	}

	return promptList, nil
}

func queryCombinationPrompts(db *sql.DB, combinationId int) ([]string, error) {
	stmt := `SELECT prompt
FROM loraCombinations
LEFT JOIN loraCombinationPrompts
ON loraCombinations.loraCombinationId = loraCombinationPrompts.loraCombinationId
WHERE loraCombinations.loraCombinationId = ?
ORDER BY seq ASC`
	rows, err := db.Query(stmt, combinationId)
	if err != nil {
		return nil, fmt.Errorf("failed to query combination prompts: %w", err)
	}

	promptList := make([]string, 0, 1)
	for rows.Next() {
		var prompt sql.NullString
		if err := rows.Scan(&prompt); err != nil {
			return nil, fmt.Errorf("failed to scan prompt: %v\n", err)
		}
		if prompt.Valid {
			promptList = append(promptList, prompt.String)
		}
	}

	return promptList, nil
}

func queryAvailableSampleImageTypes(db *sql.DB, loraId int, checkpointFilename string, promptListId int) ([]int, error) {
	stmt := `SELECT sampleType FROM sampleImages
	WHERE loraId = ? AND checkpointFilename = ? AND promptListId = ?
	ORDER BY sampleType ASC`
	rows, err := db.Query(stmt, loraId, checkpointFilename, promptListId)
	if err != nil {
		return nil, fmt.Errorf("failed to query sample types: %w", err)
	}

	sampleTypes := make([]int, 0, 9)
	for rows.Next() {
		var sampleType int
		if err := rows.Scan(&sampleType); err != nil {
			return nil, fmt.Errorf("failed to scan sample types: %w", err)
		}
		sampleTypes = append(sampleTypes, sampleType)
	}

	return sampleTypes, nil
}

func queryLoraRelativeBrowseData(db *sql.DB, loraId int) (*int, *int, error) {
	rows, err := db.Query(`SELECT prev, next
FROM (
	SELECT
		lag( loraId ) OVER ( ORDER BY loraId ) AS prev,
		loraId,
		LEAD( loraId ) OVER ( ORDER BY loraId ) AS next
	FROM loras
) WHERE loraId = ? LIMIT 1;`, loraId)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query lora browse data: %w", err)
	}
	if !rows.Next() {
		return nil, nil, nil
	}
	var prev, next sql.NullInt64
	if err := rows.Scan(&prev, &next); err != nil {
		return nil, nil, fmt.Errorf("failed to scan lora browse data: %w", err)
	}

	var prevCoale *int = nil
	var nextCoale *int = nil

	if prev.Valid {
		prevCoale = new(int)
		*prevCoale = int(prev.Int64)
	}
	if next.Valid {
		nextCoale = new(int)
		*nextCoale = int(next.Int64)
	}
	return prevCoale, nextCoale, nil
}

func queryCombinationRelativeBrowseData(db *sql.DB, combinationId int) (*int, *int, error) {
	rows, err := db.Query(`SELECT prev, next
FROM (
	SELECT
		lag( loraCombinationId ) OVER ( ORDER BY loraCombinationId ) AS prev,
		loraCombinationId,
		LEAD( loraCombinationId ) OVER ( ORDER BY loraCombinationId ) AS next
	FROM loraCombinations
) WHERE loraCombinationId = ? LIMIT 1;`, combinationId)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query combination browse data: %w", err)
	}
	if !rows.Next() {
		return nil, nil, nil
	}
	var prev, next sql.NullInt64
	if err := rows.Scan(&prev, &next); err != nil {
		return nil, nil, fmt.Errorf("failed to scan combination browse data: %w", err)
	}

	var prevCoale *int = nil
	var nextCoale *int = nil

	if prev.Valid {
		prevCoale = new(int)
		*prevCoale = int(prev.Int64)
	}
	if next.Valid {
		nextCoale = new(int)
		*nextCoale = int(next.Int64)
	}
	return prevCoale, nextCoale, nil
}

func queryCombinationComponents(db *sql.DB, combinationId int) ([]LoraCombinationsComponent, error) {
	rows, err := db.Query(`SELECT
	loraCombinationComponents.loraId,
	loraCombinationComponents.seq,
	loraCombinationComponents.strength,
	loras.name,
	loras.version,
	loras.filename
FROM loraCombinations
LEFT JOIN loraCombinationComponents
ON loraCombinations.loraCombinationId = loraCombinationComponents.loraCombinationId
LEFT JOIN loras
ON loraCombinationComponents.loraId = loras.loraId
WHERE loraCombinations.loraCombinationId = ?
ORDER BY seq ASC`, combinationId)
	if err != nil {
		return nil, fmt.Errorf("failed to select lora components: %w", err)
	}
	components := make([]LoraCombinationsComponent, 0, 6)
	for rows.Next() {
		var component LoraCombinationsComponent
		if err := rows.Scan(&component.LoraId, &component.Seq, &component.Strength, &component.Name, &component.Version, &component.Filename); err != nil {
			return nil, fmt.Errorf("failed to scan lora component: %w", err)
		}
		components = append(components, component)
	}
	return components, nil
}

func queryCheckpoints(db *sql.DB) ([]CheckpointRow, error) {
	rows, err := db.Query("SELECT checkpointFilename, name, version FROM checkpoints")
	if err != nil {
		return nil, fmt.Errorf("failed to select checkpoints: %w", err)
	}
	checkpointRows := make([]CheckpointRow, 0, 1)
	for rows.Next() {
		var checkpointRow CheckpointRow
		if err := rows.Scan(&checkpointRow.CheckpointFilename, &checkpointRow.Name, &checkpointRow.Version); err != nil {
			return nil, fmt.Errorf("failed to scan checkpoint: %w", err)
		}
		checkpointRows = append(checkpointRows, checkpointRow)
	}
	return checkpointRows, nil
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

	stmt7 := `CREATE TABLE IF NOT EXISTS
defaultCheckpoint (
	checkpointFilename TEXT NOT NULL,
	FOREIGN KEY ( checkpointFilename ) REFERENCES checkpoints ( checkpointFilename ) ON DELETE CASCADE
)`
	if _, err := db.Exec(stmt7); err != nil {
		return fmt.Errorf("failed to create default checkpoint table: %v\n", err)
	}

	stmt8 := `CREATE TABLE IF NOT EXISTS
defaultSampleType (
	sampleType INTEGER NOT NULL
)`
	if _, err := db.Exec(stmt8); err != nil {
		return fmt.Errorf("failed to create default sample type type: %v\n", err)
	}

	stmt9 := `CREATE TABLE IF NOT EXISTS
loraCombinations (
	loraCombinationId INTEGER PRIMARY KEY ASC AUTOINCREMENT
)`
	if _, err := db.Exec(stmt9); err != nil {
		return fmt.Errorf("failed to create lora combination table: %v\n", err)
	}

	stmt10 := `CREATE TABLE IF NOT EXISTS
loraCombinationComponents (
	loraCombinationId REFERENCES loraCombinations ( loraCombinationId ) ON DELETE CASCADE,
	loraId REFERENCES loras ( loraId ) ON DELETE CASCADE,
	seq INTEGER NOT NULL,
	strength INTEGER NOT NULL,
	UNIQUE ( loraCombinationId, loraId, seq ) ON CONFLICT FAIL
)`
	if _, err := db.Exec(stmt10); err != nil {
		return fmt.Errorf("failed to create lora combination table: %v\n", err)
	}

	stmt11 := `CREATE TABLE IF NOT EXISTS
loraCombinationSampleImages (
	loraCombinationId REFERENCES loraCombinations ( loraCombinationId ) ON DELETE CASCADE,
	checkpointFilename REFERENCES checkpoints ( checkpointFilename ) ON DELETE CASCADE,
	sampleType INTEGER NOT NULL,
	sampleImage BLOB NOT NULL,
	UNIQUE ( loraCombinationId, checkpointFilename, sampleType ) ON CONFLICT REPLACE
)`
	if _, err := db.Exec(stmt11); err != nil {
		return fmt.Errorf("failed to create lora combination table: %v\n", err)
	}

	stmt12 := `CREATE TABLE IF NOT EXISTS
loraCombinationPrompts (
	loraCombinationId REFERENCES loraCombinations ( loraCombinationId ) ON DELETE CASCADE,
	seq INTEGER NOT NULL,
	prompt TEXT NOT NULL,
	UNIQUE ( loraCombinationId, seq ) ON CONFLICT FAIL,
	CHECK ( prompt <> "" )
)`
	if _, err := db.Exec(stmt12); err != nil {
		return fmt.Errorf("failed to create lora combination table: %v\n", err)
	}

	stmt13 := `CREATE TABLE IF NOT EXISTS
loraCombinationTests (
	loraCombinationTestId INTEGER PRIMARY KEY ASC AUTOINCREMENT
)`
	if _, err := db.Exec(stmt13); err != nil {
		return fmt.Errorf("failed to create lora combination test table: %v\n", err)
	}

	stmt14 := `CREATE TABLE IF NOT EXISTS
loraCombinationTestComponents (
	loraCombinationTestId REFERENCES loraCombinationTests ( loraCombinationTestId ) ON DELETE CASCADE,
	componentSeq INTEGER NOT NULL,
	loraId REFERENCES loras ( loraId ) ON DELETE CASCADE,
	UNIQUE ( loraCombinationTestId, componentSeq ) ON CONFLICT REPLACE
)`
	if _, err := db.Exec(stmt14); err != nil {
		return fmt.Errorf("failed to create lora combination test component table: %v\n", err)
	}

	stmt15 := `CREATE TABLE IF NOT EXISTS
loraCombinationTestTrials (
	loraCombinationTestId REFERENCES loraCombinationTests ( loraCombinationTestId ) ON DELETE CASCADE,
	trialIndex INTEGER NOT NULL,
	UNIQUE ( loraCombinationTestId, trialIndex ) ON CONFLICT FAIL
)`
	if _, err := db.Exec(stmt15); err != nil {
		return fmt.Errorf("failed to create lora combination test trial table: %v\n", err)
	}

	stmt16 := `CREATE TABLE IF NOT EXISTS
loraCombinationTestTrialParameters (
	loraCombinationTestId INTEGER NOT NULL,
	trialIndex INTEGER NOT NULL,
	componentSeq INTEGER NOT NULL,
	strength INTEGER NOT NULL,
	FOREIGN KEY ( loraCombinationTestId, trialIndex ) REFERENCES loraCombinationTestTrials ( loraCombinationTestId, trialIndex ) ON DELETE CASCADE,
	FOREIGN KEY ( loraCombinationTestId, componentSeq ) REFERENCES loraCombinationTestComponents ( loraCombinationTestId, componentSeq ) ON DELETE CASCADE,
	UNIQUE ( loraCombinationTestId, trialIndex, componentSeq ) ON CONFLICT REPLACE
)`
	if _, err := db.Exec(stmt16); err != nil {
		return fmt.Errorf("failed to create lora combination test trial parameter table: %v\n", err)
	}

	stmt17 := `CREATE TABLE IF NOT EXISTS
	loraCombinationTestTrialSampleImages (
		loraCombinationTestId INTEGER NOT NULL,
		trialIndex INTEGER NOT NULL,
		checkpointFilename REFERENCES checkpoints ( checkpointFilename ) ON DELETE CASCADE,
		sampleType INTEGER NOT NULL,
		sampleImage BLOB NOT NULL,
		FOREIGN KEY ( loraCombinationTestId, trialIndex ) REFERENCES loraCombinationTestTrials ( loraCombinationTestId, trialIndex ) ON DELETE CASCADE,
		UNIQUE ( loraCombinationTestId, trialIndex, checkpointFilename, sampleType ) ON CONFLICT REPLACE
	)`
	if _, err := db.Exec(stmt17); err != nil {
		return fmt.Errorf("failed to create lora combination test trial sample image table: %v\n", err)
	}

	stmt18 := `CREATE TABLE IF NOT EXISTS
loraCombinationTestPrompts (
	loraCombinationTestId REFERENCES loraCombinationTests ( loraCombinationTestId ) ON DELETE CASCADE,
	promptSeq INTEGER NOT NULL,
	prompt TEXT NOT NULL,
	UNIQUE ( loraCombinationTestId, promptSeq ) ON CONFLICT REPLACE,
	CHECK ( prompt <> "" )
)`
	if _, err := db.Exec(stmt18); err != nil {
		return fmt.Errorf("failed to create lora combination test trial sample image table: %v\n", err)
	}

	stmt19 := `CREATE TABLE IF NOT EXISTS
loraCombinationTestTrialParameterFilters (
	loraCombinationTestId INTEGER NOT NULL,
	componentSeq INTEGER NOT NULL,
	op TEXT CHECK ( op IN ( "eq", "lt", "gt", "le", "ge" ) ) NOT NULL,
	value INTEGER NOT NULL,
	FOREIGN KEY ( loraCombinationTestId, componentSeq ) REFERENCES loraCombinationTestComponents ( loraCombinationTestId, componentSeq ) ON DELETE CASCADE
)`
	if _, err := db.Exec(stmt19); err != nil {
		return fmt.Errorf("failed to create lora combination test trial parameter table: %v\n", err)
	}
	return nil
}
