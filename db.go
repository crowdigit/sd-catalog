package main

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
)

type LoraRow struct {
	Name     string `json:"name"`
	Url      string `json:"url"`
	Version  string `json:"version"`
	Filename string `json:"filename"`
}

type LoraRowV2 struct {
	LoraId           int    `json:"loraId"`
	Name             string `json:"name"`
	Version          string `json:"version"`
	Url              string `json:"url"`
	CivitaiModelId   int    `json:"civitaiModelId"`
	CivitaiVersionId int    `json:"civitaiVersionId"`
	Filename         string `json:"filename"`
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

func insertLoraRow(db DB, title, url, version, filename string, prompts [][]string, urlpreview []byte) (int64, error) {
	stmt := `INSERT INTO loras ( name, url, urlpreview, version, filename ) VALUES ( ?, ?, ?, ?, ? )`
	result, err := db.Exec(stmt, title, url, urlpreview, version, filename)
	if err != nil {
		return 0, fmt.Errorf("failed to insert lora: %w", err)
	}

	loraId, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get inserted lora row id: %w", err)
	}

	promptListStmt := `INSERT INTO promptLists ( loraId, promptListId ) VALUES ( ?, ? )`
	promptStmt := `INSERT INTO prompts ( loraId, promptListId, seq, prompt ) VALUES ( ?, ?, ?, ? )`
	for promptListIndex, promptList := range prompts {
		if _, err := db.Exec(promptListStmt, loraId, promptListIndex+1); err != nil {
			return 0, fmt.Errorf("failed to execute insert prompt list statement: %w", err)
		}
		for promptIndex, prompt := range promptList {
			if _, err := db.Exec(promptStmt, loraId, promptListIndex+1, promptIndex+1, prompt); err != nil {
				return 0, fmt.Errorf("failed to execute insert prompt statement: %w", err)
			}
		}
	}

	return loraId, nil
}

func insertSampleImage(db DB, loraId int, promptlistId int, checkpointFilename string, sampleType int, sampleImage []byte) error {
	stmt := `INSERT INTO sampleImages ( loraId, promptlistId, checkpointFilename, sampleType, sampleImage ) VALUES ( ?, ?, ?, ?, ? )`
	_, err := db.Exec(stmt, loraId, promptlistId, checkpointFilename, sampleType, sampleImage)
	if err != nil {
		return fmt.Errorf("failed to execute insert sample image statement: %w", err)
	}
	return nil
}

func insertSampleImageV2(db DB, loraId int, promptlistId int, checkpointFilename string, sampleType int, sampleImage []byte) error {
	stmt := `INSERT INTO sampleImagesV2 ( loraId, promptlistId, checkpointFilename, sampleType, sampleImage ) VALUES ( ?, ?, ?, ?, ? )`
	_, err := db.Exec(stmt, loraId, promptlistId, checkpointFilename, sampleType, sampleImage)
	if err != nil {
		return fmt.Errorf("failed to execute insert sample image statement: %w", err)
	}
	return nil
}

func insertCheckpoint(db DB, checkpointFilename string, name string, version string) error {
	stmt := `INSERT INTO checkpoints ( checkpointFilename, name, version ) VALUES ( ?, ?, ? )`
	_, err := db.Exec(stmt, checkpointFilename, name, version)
	if err != nil {
		return fmt.Errorf("failed to insert checkpoint row: %w", err)
	}
	return nil
}

func queryDefaultCheckpoint(db DB) (string, error) {
	rows, err := db.Query("SELECT checkpointFilename FROM defaultCheckpoint LIMIT 1")
	if err != nil {
		return "", fmt.Errorf("failed to select from defaultCheckpoint: %w", err)
	}
	defer rows.Close()
	var defaultCheckpoint string
	if !rows.Next() {
		return "", fmt.Errorf("failed to scan default checkpoint: %w", err)
	} else if err := rows.Scan(&defaultCheckpoint); err != nil {
		return "", fmt.Errorf("failed to scan default checkpoint: %w", err)
	}
	return defaultCheckpoint, nil
}

func queryLora(db DB, loraId int) (*LoraRow, error) {
	rows, err := db.Query("SELECT name, url, version, filename FROM loras WHERE loraId = ?", loraId)
	if err != nil {
		return nil, fmt.Errorf("failed to select lora: %w", err)
	}
	defer rows.Close()
	var lora LoraRow
	if !rows.Next() {
		return nil, nil
	} else if err := rows.Scan(&lora.Name, &lora.Url, &lora.Version, &lora.Filename); err != nil {
		return nil, fmt.Errorf("failed to scan lora: %v\n", err)
	}
	return &lora, nil
}

func queryLoraV2(db DB, loraId int) (*LoraRowV2, error) {
	rows, err := db.Query("SELECT * FROM lorasV2 WHERE loraId = ?", loraId)
	if err != nil {
		return nil, fmt.Errorf("failed to select lora: %w", err)
	}
	defer rows.Close()
	var lora LoraRowV2
	if !rows.Next() {
		return nil, nil
	} else if err := rows.Scan(&lora.LoraId, &lora.Name, &lora.Version, &lora.Url, &lora.CivitaiModelId, &lora.CivitaiVersionId, &lora.Filename); err != nil {
		return nil, fmt.Errorf("failed to scan lora: %v\n", err)
	}
	return &lora, nil
}

func queryLoraPrompts(db DB, loraId int) ([]PromptListItem, error) {
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
	defer rows.Close()

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

func queryLoraPromptsV2(db DB, loraId int) ([]PromptListItem, error) {
	stmt := `SELECT promptListsV2.promptListId, promptsV2.prompt
FROM promptListsV2
LEFT JOIN promptsV2
ON promptListsV2.loraId = promptsV2.loraId AND promptListsV2.promptListId = promptsV2.promptListId
WHERE promptListsV2.loraId = ?
ORDER BY promptListsV2.promptListId ASC, promptsV2.seq ASC`
	rows, err := db.Query(stmt, loraId)
	if err != nil {
		return nil, fmt.Errorf("failed to query prompts: %w", err)
	}
	defer rows.Close()

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

func queryCombinationPrompts(db DB, combinationId int) ([]string, error) {
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
	defer rows.Close()

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

func queryCombinationPromptsV2(db DB, combinationId int) ([]string, error) {
	stmt := `SELECT prompt
FROM loraCombinationsV2
LEFT JOIN loraCombinationPromptsV2
ON loraCombinationsV2.loraCombinationId = loraCombinationPromptsV2.loraCombinationId
WHERE loraCombinationsV2.loraCombinationId = ?
ORDER BY seq ASC`
	rows, err := db.Query(stmt, combinationId)
	if err != nil {
		return nil, fmt.Errorf("failed to query combination prompts: %w", err)
	}
	defer rows.Close()

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

func queryAvailableSampleImageTypes(db DB, loraId int, checkpointFilename string, promptListId int) ([]int, error) {
	stmt := `SELECT sampleType FROM sampleImages
	WHERE loraId = ? AND checkpointFilename = ? AND promptListId = ?
	ORDER BY sampleType ASC`
	rows, err := db.Query(stmt, loraId, checkpointFilename, promptListId)
	if err != nil {
		return nil, fmt.Errorf("failed to query sample types: %w", err)
	}
	defer rows.Close()

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

func queryAvailableSampleImageTypesV2(db DB, loraId int, checkpointFilename string, promptListId int) ([]int, error) {
	stmt := `SELECT sampleType FROM sampleImagesV2
	WHERE loraId = ? AND checkpointFilename = ? AND promptListId = ?
	ORDER BY sampleType ASC`
	rows, err := db.Query(stmt, loraId, checkpointFilename, promptListId)
	if err != nil {
		return nil, fmt.Errorf("failed to query sample types: %w", err)
	}
	defer rows.Close()

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

func queryLoraRelativeBrowseData(db DB, loraId int) (*int, *int, error) {
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
	defer rows.Close()
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

func queryLoraRelativeBrowseDataV2(db DB, loraId int) (*int, *int, error) {
	rows, err := db.Query(`SELECT prev, next
FROM (
	SELECT
		lag( loraId ) OVER ( ORDER BY loraId ) AS prev,
		loraId,
		LEAD( loraId ) OVER ( ORDER BY loraId ) AS next
	FROM lorasV2
) WHERE loraId = ? LIMIT 1;`, loraId)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query lora browse data: %w", err)
	}
	defer rows.Close()
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

func queryLoraPreviewListV2(db DB, loraId int) ([]int, error) {
	rows, err := db.Query("SELECT seq FROM previewV2 WHERE loraId = ?", loraId)
	if err != nil {
		return nil, fmt.Errorf("failed to query lora preview list: %w\n", err)
	}
	defer rows.Close()
	var seqs []int
	for rows.Next() {
		var seq int
		if err := rows.Scan(&seq); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w\n", err)
		}
		seqs = append(seqs, seq)
	}
	return seqs, nil
}

func queryCombinationRelativeBrowseData(db DB, combinationId int) (*int, *int, error) {
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
	defer rows.Close()
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

func queryCombinationRelativeBrowseDataV2(db DB, combinationId int) (*int, *int, error) {
	rows, err := db.Query(`SELECT prev, next
FROM (
	SELECT
		lag( loraCombinationId ) OVER ( ORDER BY loraCombinationId ) AS prev,
		loraCombinationId,
		LEAD( loraCombinationId ) OVER ( ORDER BY loraCombinationId ) AS next
	FROM loraCombinationsV2
) WHERE loraCombinationId = ? LIMIT 1;`, combinationId)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query combination browse data: %w", err)
	}
	defer rows.Close()
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

func queryCombinationComponents(db DB, combinationId int) ([]LoraCombinationsComponent, error) {
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
	defer rows.Close()
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

func queryCombinationComponentsV2(db DB, combinationId int) ([]LoraCombinationsComponent, error) {
	rows, err := db.Query(`SELECT
	loraCombinationComponentsV2.loraId,
	loraCombinationComponentsV2.seq,
	loraCombinationComponentsV2.strength,
	lorasV2.name,
	lorasV2.version,
	lorasV2.filename
FROM loraCombinationsV2
LEFT JOIN loraCombinationComponentsV2
ON loraCombinationsV2.loraCombinationId = loraCombinationComponentsV2.loraCombinationId
LEFT JOIN lorasV2
ON loraCombinationComponentsV2.loraId = lorasV2.loraId
WHERE loraCombinationsV2.loraCombinationId = ?
ORDER BY seq ASC`, combinationId)
	if err != nil {
		return nil, fmt.Errorf("failed to select lora components: %w", err)
	}
	defer rows.Close()
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

func queryCheckpoints(db DB) ([]CheckpointRow, error) {
	rows, err := db.Query("SELECT checkpointFilename, name, version FROM checkpoints")
	if err != nil {
		return nil, fmt.Errorf("failed to select checkpoints: %w", err)
	}
	defer rows.Close()
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

func queryLoraCombinationAvailableSampleTypes(db DB, combinationId int, checkpointFilename string) ([]int, error) {
	stmt := `SELECT sampleType
		FROM loraCombinationSampleImages
		WHERE
		loraCombinationSampleImages.loraCombinationId = ?
		AND checkpointFilename = ?
		ORDER BY sampleType ASC`
	rows, err := db.Query(stmt, combinationId, checkpointFilename)
	defer rows.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to query available sample images: %w\n", err)
	}

	availableSampleTypes := make([]int, 0, 9)
	for rows.Next() {
		var sampleType int
		if err := rows.Scan(&sampleType); err != nil {
			return nil, fmt.Errorf("failed to scan available sample images: %w", err)
		}
		availableSampleTypes = append(availableSampleTypes, sampleType)
	}
	return availableSampleTypes, nil
}

func queryLoraCombinationAvailableSampleTypesV2(db DB, combinationId int, checkpointFilename string) ([]int, error) {
	stmt := `SELECT sampleType
		FROM loraCombinationSampleImagesV2
		WHERE
		loraCombinationSampleImagesV2.loraCombinationId = ?
		AND checkpointFilename = ?
		ORDER BY sampleType ASC`
	rows, err := db.Query(stmt, combinationId, checkpointFilename)
	defer rows.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to query available sample images: %w\n", err)
	}

	availableSampleTypes := make([]int, 0, 9)
	for rows.Next() {
		var sampleType int
		if err := rows.Scan(&sampleType); err != nil {
			return nil, fmt.Errorf("failed to scan available sample images: %w", err)
		}
		availableSampleTypes = append(availableSampleTypes, sampleType)
	}
	return availableSampleTypes, nil
}

func queryLoraCount(db DB) (int, error) {
	rows, err := db.Query("SELECT COUNT(*) AS num FROM loras")
	if err != nil {
		return 0, fmt.Errorf("failed to select lora IDs: %w", err)
	}
	defer rows.Close()
	rows.Next()
	var num int
	if err := rows.Scan(&num); err != nil {
		return 0, fmt.Errorf("failed to scan lora ID from row: %w", err)
	}
	return num, nil
}

func queryLoraCountV2(db DB) (int, error) {
	rows, err := db.Query("SELECT COUNT(*) AS num FROM lorasV2")
	if err != nil {
		return 0, fmt.Errorf("failed to select lora IDs: %w", err)
	}
	defer rows.Close()
	rows.Next()
	var num int
	if err := rows.Scan(&num); err != nil {
		return 0, fmt.Errorf("failed to scan lora ID from row: %w", err)
	}
	return num, nil
}

func queryLoraByPage(db DB, page int) ([]int, []string, []string, error) {
	offset := browseLoraPageLimit * page
	rows, err := db.Query(
		"SELECT loraId, name, version FROM loras ORDER BY loraId ASC LIMIT ? OFFSET ?",
		browseLoraPageLimit,
		offset)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to select lora IDs: %w", err)
	}
	defer rows.Close()

	loraIds := make([]int, 0, browseLoraPageLimit)
	names := make([]string, 0, browseLoraPageLimit)
	versions := make([]string, 0, browseLoraPageLimit)
	for rows.Next() {
		var loraId int64
		var name string
		var version string
		if err := rows.Scan(&loraId, &name, &version); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to scan lora ID from row: %w", err)
		}
		loraIds = append(loraIds, int(loraId))
		names = append(names, name)
		versions = append(versions, version)
	}
	return loraIds, names, versions, nil
}

func queryLoraByPageV2(db DB, page int) ([]int, []string, []string, error) {
	offset := browseLoraPageLimit * page
	rows, err := db.Query(
		"SELECT loraId, name, version FROM lorasV2 ORDER BY loraId ASC LIMIT ? OFFSET ?",
		browseLoraPageLimit,
		offset)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to select lora IDs: %w", err)
	}
	defer rows.Close()

	loraIds := make([]int, 0, browseLoraPageLimit)
	names := make([]string, 0, browseLoraPageLimit)
	versions := make([]string, 0, browseLoraPageLimit)
	for rows.Next() {
		var loraId int64
		var name string
		var version string
		if err := rows.Scan(&loraId, &name, &version); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to scan lora ID from row: %w", err)
		}
		loraIds = append(loraIds, int(loraId))
		names = append(names, name)
		versions = append(versions, version)
	}
	return loraIds, names, versions, nil
}

func queryLoraCombinationCount(db DB) (int, error) {
	rows, err := db.Query("SELECT COUNT(*) AS num FROM loraCombinations")
	if err != nil {
		return 0, fmt.Errorf("failed to select combination IDs: %w", err)
	}
	defer rows.Close()
	rows.Next()
	var count int
	if err := rows.Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to scan combination ID from row: %w", err)
	}
	return count, nil
}

func queryLoraCombinationCountV2(db DB) (int, error) {
	rows, err := db.Query("SELECT COUNT(*) AS num FROM loraCombinationsV2")
	if err != nil {
		return 0, fmt.Errorf("failed to select combination IDs: %w", err)
	}
	defer rows.Close()
	rows.Next()
	var count int
	if err := rows.Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to scan combination ID from row: %w", err)
	}
	return count, nil
}

func queryLoraCombinationByPage(db DB, page int) ([]int, error) {
	offset := browseCombinationLimit * page
	rows, err := db.Query(
		"SELECT loraCombinationId FROM loraCombinations ORDER BY loraCombinationId ASC LIMIT ? OFFSET ?",
		browseCombinationLimit,
		offset)
	if err != nil {
		return nil, fmt.Errorf("failed to select combination IDs: %w", err)
	}
	defer rows.Close()

	combinationIds := make([]int, 0, browseCombinationLimit)
	for rows.Next() {
		var combinationId int64
		if err := rows.Scan(&combinationId); err != nil {
			return nil, fmt.Errorf("failed to scan combination ID from row: %w", err)
		}
		combinationIds = append(combinationIds, int(combinationId))
	}

	return combinationIds, nil
}

func queryLoraCombinationByPageV2(db DB, page int) ([]int, error) {
	offset := browseCombinationLimit * page
	rows, err := db.Query(
		"SELECT loraCombinationId FROM loraCombinationsV2 ORDER BY loraCombinationId ASC LIMIT ? OFFSET ?",
		browseCombinationLimit,
		offset)
	if err != nil {
		return nil, fmt.Errorf("failed to select combination IDs: %w", err)
	}
	defer rows.Close()

	combinationIds := make([]int, 0, browseCombinationLimit)
	for rows.Next() {
		var combinationId int64
		if err := rows.Scan(&combinationId); err != nil {
			return nil, fmt.Errorf("failed to scan combination ID from row: %w", err)
		}
		combinationIds = append(combinationIds, int(combinationId))
	}

	return combinationIds, nil
}

func queryLoraPreview(db DB, loraId int) ([]byte, error) {
	rows, err := db.Query("SELECT urlpreview FROM loras WHERE loraId = ?", loraId)
	if err != nil {
		return nil, fmt.Errorf("failed to select lora: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	var urlpreview []byte
	if err := rows.Scan(&urlpreview); err != nil {
		return nil, fmt.Errorf("failed to scan urlpreview image from query result: %v\n", err)
	}
	return urlpreview, nil
}

func queryLoraPreviewV2(db DB, loraId int, seq int) ([]byte, string, error) {
	rows, err := db.Query("SELECT image, type FROM previewV2 WHERE loraId = ? AND seq = ?", loraId, seq)
	if err != nil {
		return nil, "", fmt.Errorf("failed to select lora preview image: %w", err)
	}
	defer rows.Close()
	var image []byte
	var type_ string
	if rows.Next() {
		if err := rows.Scan(&image, &type_); err != nil {
			return nil, "", fmt.Errorf("failed to scan row: %w\n", err)
		}
	}
	return image, type_, nil
}

func queryLoraUrl(db DB, loraId int) (string, error) {
	rows, err := db.Query("SELECT url FROM loras WHERE loraId = ?", loraId)
	if err != nil {
		return "", fmt.Errorf("failed to select lora: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return "", nil
	}
	var url string
	if err := rows.Scan(&url); err != nil {
		return "", fmt.Errorf("failed to scan urlpreview image from query result: %v\n", err)
	}
	return url, nil
}

func queryLoraPreviewSampleImage(db DB, loraId, sampleType int) ([]byte, error) {
	stmt := `SELECT sampleImage
FROM sampleImages
INNER JOIN defaultCheckpoint
ON sampleImages.checkpointFilename = defaultCheckpoint.checkpointFilename
WHERE sampleImages.loraId = ?
AND sampleImages.sampleType = ?
ORDER BY sampleImages.promptListId ASC
LIMIT 1;`
	rows, err := db.Query(stmt, loraId, sampleType)
	if err != nil {
		log.Printf("failed to query default sample image: %v\n", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	var image []byte
	if err := rows.Scan(&image); err != nil {
		return nil, fmt.Errorf("failed to scan sample image from query result: %w", err)
	}
	return image, nil
}

func queryLoraPreviewSampleImageV2(db DB, loraId, sampleType int) ([]byte, error) {
	stmt := `SELECT sampleImage
FROM sampleImagesV2
INNER JOIN defaultCheckpoint
ON sampleImagesV2.checkpointFilename = defaultCheckpoint.checkpointFilename
WHERE sampleImagesV2.loraId = ?
AND sampleImagesV2.sampleType = ?
ORDER BY sampleImagesV2.promptListId ASC
LIMIT 1;`
	rows, err := db.Query(stmt, loraId, sampleType)
	if err != nil {
		log.Printf("failed to query default sample image: %v\n", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	var image []byte
	if err := rows.Scan(&image); err != nil {
		return nil, fmt.Errorf("failed to scan sample image from query result: %w", err)
	}
	return image, nil
}

func queryLoraSampleImage(db DB, loraId int, promptListId int, checkpointFilename string, sampleType int) ([]byte, error) {
	rows, err := db.Query(
		`SELECT sampleImage FROM sampleImages WHERE loraId = ? AND promptListId = ? AND checkpointFilename = ? AND sampleType = ? LIMIT 1`,
		loraId, promptListId, checkpointFilename, sampleType)
	if err != nil {
		return nil, fmt.Errorf("failed to query sample image: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	var image []byte
	if err := rows.Scan(&image); err != nil {
		return nil, fmt.Errorf("failed to scan sample image: %w", err)
	}
	return image, nil
}

func queryLoraSampleImageV2(db DB, loraId int, promptListId int, checkpointFilename string, sampleType int) ([]byte, error) {
	rows, err := db.Query(
		`SELECT sampleImage FROM sampleImagesV2 WHERE loraId = ? AND promptListId = ? AND checkpointFilename = ? AND sampleType = ? LIMIT 1`,
		loraId, promptListId, checkpointFilename, sampleType)
	if err != nil {
		return nil, fmt.Errorf("failed to query sample image: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	var image []byte
	if err := rows.Scan(&image); err != nil {
		return nil, fmt.Errorf("failed to scan sample image: %w", err)
	}
	return image, nil
}

func queryCombinationSampleImage(db DB, combinationId int, checkpointFilename string, sampleType int) ([]byte, error) {
	rows, err := db.Query(
		`SELECT sampleImage FROM loraCombinationSampleImages WHERE loraCombinationId = ? AND checkpointFilename = ? AND sampleType = ? LIMIT 1`,
		combinationId, checkpointFilename, sampleType)
	if err != nil {
		return nil, fmt.Errorf("failed to query sample image: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	var image []byte
	if err := rows.Scan(&image); err != nil {
		return nil, fmt.Errorf("failed to scan sample image: %w", err)
	}
	return image, nil
}

func queryCombinationSampleImageV2(db DB, combinationId int, checkpointFilename string, sampleType int) ([]byte, error) {
	rows, err := db.Query(
		`SELECT sampleImage FROM loraCombinationSampleImagesV2 WHERE loraCombinationId = ? AND checkpointFilename = ? AND sampleType = ? LIMIT 1`,
		combinationId, checkpointFilename, sampleType)
	if err != nil {
		return nil, fmt.Errorf("failed to query sample image: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	var image []byte
	if err := rows.Scan(&image); err != nil {
		return nil, fmt.Errorf("failed to scan sample image: %w", err)
	}
	return image, nil
}

func queryCombinationRandomPromptImageV2(db DB, combinationId int, checkpointFilename string, randomPromptListId int) ([]byte, error) {
	rows, err := db.Query(
		`SELECT image FROM loraCombinationRandomPromptImagesV2 WHERE loraCombinationId = ? AND checkpointFilename = ? AND randomPromptListId = ? LIMIT 1`,
		combinationId, checkpointFilename, randomPromptListId)
	if err != nil {
		return nil, fmt.Errorf("failed to query image: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	var image []byte
	if err := rows.Scan(&image); err != nil {
		return nil, fmt.Errorf("failed to scan image: %w", err)
	}
	return image, nil
}

func queryCombinationPreviewSampleImage(db DB, combinationId int, sampleType int) ([]byte, error) {
	stmt := `SELECT loraCombinationSampleImages.sampleImage
FROM loraCombinationSampleImages
INNER JOIN defaultCheckpoint
ON loraCombinationSampleImages.checkpointFilename = defaultCheckpoint.checkpointFilename
WHERE loraCombinationSampleImages.loraCombinationId = ?
AND loraCombinationSampleImages.sampleType = ?;`
	rows, err := db.Query(stmt, combinationId, sampleType)
	if err != nil {
		return nil, fmt.Errorf("failed to query default sample image: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	var image []byte
	if err := rows.Scan(&image); err != nil {
		return nil, fmt.Errorf("failed to scan sample image from query result: %w", err)
	}
	return image, nil
}

func queryCombinationPreviewSampleImageV2(db DB, combinationId int, sampleType int) ([]byte, error) {
	stmt := `SELECT loraCombinationSampleImagesV2.sampleImage
FROM loraCombinationSampleImagesV2
INNER JOIN defaultCheckpoint
ON loraCombinationSampleImagesV2.checkpointFilename = defaultCheckpoint.checkpointFilename
WHERE loraCombinationSampleImagesV2.loraCombinationId = ?
AND loraCombinationSampleImagesV2.sampleType = ?;`
	rows, err := db.Query(stmt, combinationId, sampleType)
	if err != nil {
		return nil, fmt.Errorf("failed to query default sample image: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	var image []byte
	if err := rows.Scan(&image); err != nil {
		return nil, fmt.Errorf("failed to scan sample image from query result: %w", err)
	}
	return image, nil
}

func initDB(db DB) error {
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

	stmt21 := `CREATE TABLE IF NOT EXISTS
lorasV2 (
	loraId INTEGER PRIMARY KEY ASC AUTOINCREMENT,
	name TEXT NOT NULL,
	version TEXT NOT NULL,
	url TEXT NOT NULL,
	civitaiModelId INTEGER NOT NULL,
	civitaiVersionId INTEGER NOT NULL,
	filename TEXT NOT NULL,
	UNIQUE ( civitaiModelId, civitaiVersionId ) ON CONFLICT REPLACE,
	UNIQUE ( filename ) ON CONFLICT FAIL,
	CHECK ( filename <> "" )
)`
	if _, err := db.Exec(stmt21); err != nil {
		return fmt.Errorf("failed to create lora v2 table: %v\n", err)
	}

	stmt22 := `CREATE TABLE IF NOT EXISTS
previewV2 (
	loraId REFERENCES lorasV2 ( loraId ) ON DELETE CASCADE,
	seq INTEGER NOT NULL,
	image BLOB NOT NULL,
	type TEXT NOT NULL,
	UNIQUE ( loraId, seq ) ON CONFLICT REPLACE,
	CHECK ( seq != 0 AND type <> "" )
)`
	if _, err := db.Exec(stmt22); err != nil {
		return fmt.Errorf("failed to create lora preview v2 table: %v\n", err)
	}

	stmt23 := `CREATE TABLE IF NOT EXISTS
downloadWipV2 (
	loraId INTEGER NOT NULL,
	UNIQUE ( loraId ) ON CONFLICT FAIL,
	FOREIGN KEY ( loraId ) REFERENCES lorasV2 ( loraId ) ON DELETE RESTRICT
)`
	if _, err := db.Exec(stmt23); err != nil {
		return fmt.Errorf("failed to create lora download WIP v2 table: %v\n", err)
	}

	stmt24 := `CREATE TABLE IF NOT EXISTS
promptListsV2 (
	loraId REFERENCES lorasV2 ( loraId ) ON DELETE CASCADE,
	promptListId INTEGER NOT NULL,
	UNIQUE ( loraId, promptListId ) ON CONFLICT REPLACE
)`
	if _, err := db.Exec(stmt24); err != nil {
		return fmt.Errorf("failed to create prmopt lists v2 table: %v\n", err)
	}
	stmt25 := `CREATE TABLE IF NOT EXISTS
promptsV2 (
    loraId INTEGER NOT NULL,
    promptListId INTEGER NOT NULL,
    seq INTEGER NOT NULL,
    prompt TEXT NOT NULL,
    FOREIGN KEY ( loraId, promptListId ) REFERENCES promptListsV2 ( loraId, promptListId ) ON DELETE CASCADE,
    UNIQUE ( loraId, promptListId, seq ) ON CONFLICT REPLACE,
    CHECK ( seq != 0 AND prompt <> "")
)`
	if _, err := db.Exec(stmt25); err != nil {
		return fmt.Errorf("failed to execute create prompts v2 table: %w", err)
	}

	stmt26 := `CREATE TABLE IF NOT EXISTS
sampleImagesV2 (
    loraId INTEGER NOT NULL,
    promptListId INTEGER NOT NULL,
    checkpointFilename TEXT NOT NULL,
    sampleType INTEGER NOT NULL,
    sampleImage BLOB NOT NULL,
    FOREIGN KEY ( loraId, promptListId ) REFERENCES promptListsV2 ( loraId, promptListId ) ON DELETE CASCADE,
    FOREIGN KEY ( checkpointFilename ) REFERENCES checkpoints ( checkpointFilename ) ON DELETE CASCADE,
    UNIQUE ( loraId, promptListId, checkpointFilename, sampleType ) ON CONFLICT REPLACE
)`
	if _, err := db.Exec(stmt26); err != nil {
		return fmt.Errorf("failed to execute create sample images table v2: %w", err)
	}

	stmt27 := `CREATE TABLE IF NOT EXISTS
loraCombinationsV2 (
	loraCombinationId INTEGER PRIMARY KEY ASC AUTOINCREMENT
)`
	if _, err := db.Exec(stmt27); err != nil {
		return fmt.Errorf("failed to create lora combination table: %v\n", err)
	}

	stmt28 := `CREATE TABLE IF NOT EXISTS
loraCombinationComponentsV2 (
	loraCombinationId REFERENCES loraCombinationsV2 ( loraCombinationId ) ON DELETE CASCADE,
	loraId REFERENCES lorasV2 ( loraId ) ON DELETE CASCADE,
	seq INTEGER NOT NULL,
	strength INTEGER NOT NULL,
	UNIQUE ( loraCombinationId, loraId, seq ) ON CONFLICT FAIL
)`
	if _, err := db.Exec(stmt28); err != nil {
		return fmt.Errorf("failed to create lora combination table: %v\n", err)
	}

	stmt29 := `CREATE TABLE IF NOT EXISTS
loraCombinationSampleImagesV2 (
	loraCombinationId REFERENCES loraCombinationsV2 ( loraCombinationId ) ON DELETE CASCADE,
	checkpointFilename REFERENCES checkpoints ( checkpointFilename ) ON DELETE CASCADE,
	sampleType INTEGER NOT NULL,
	sampleImage BLOB NOT NULL,
	UNIQUE ( loraCombinationId, checkpointFilename, sampleType ) ON CONFLICT REPLACE
)`
	if _, err := db.Exec(stmt29); err != nil {
		return fmt.Errorf("failed to create lora combination table: %v\n", err)
	}

	stmt30 := `CREATE TABLE IF NOT EXISTS
loraCombinationPromptsV2 (
	loraCombinationId REFERENCES loraCombinationsV2 ( loraCombinationId ) ON DELETE CASCADE,
	seq INTEGER NOT NULL,
	prompt TEXT NOT NULL,
	UNIQUE ( loraCombinationId, seq ) ON CONFLICT FAIL,
	CHECK ( prompt <> "" )
)`
	if _, err := db.Exec(stmt30); err != nil {
		return fmt.Errorf("failed to create lora combination table: %v\n", err)
	}

	stmt31 := `CREATE TABLE IF NOT EXISTS
randomPromptListsV2 (
	randomPromptListId INTEGER PRIMARY KEY ASC AUTOINCREMENT
)`
	if _, err := db.Exec(stmt31); err != nil {
		return fmt.Errorf("failed to create random prompt list table: %v\n", err)
	}

	stmt32 := `CREATE TABLE IF NOT EXISTS
randomPromptsV2 (
	randomPromptListId REFERENCES randomPromptListsV2 ( randomPromptListId ) ON DELETE CASCADE,
	seq INTEGER NOT NULL,
	prompt TEXT NOT NULL,
	UNIQUE ( randomPromptListId, seq ) ON CONFLICT REPLACE,
	CHECK ( seq != 0 AND prompt <> "")
)`
	if _, err := db.Exec(stmt32); err != nil {
		return fmt.Errorf("failed to create random prompt list table: %v\n", err)
	}

	stmt33 := `CREATE TABLE IF NOT EXISTS
loraCombinationRandomPromptImagesV2 (
	loraCombinationId REFERENCES loraCombinationsV2 ( loraCombinationId ) ON DELETE CASCADE,
	checkpointFilename REFERENCES checkpoints ( checkpointFilename ) ON DELETE CASCADE,
	randomPromptListId REFERENCES randomPromptListsV2 ( randomPromptListId ) ON DELETE CASCADE,
	image BLOB NOT NULL,
	UNIQUE ( loraCombinationId, checkpointFilename, randomPromptListId ) ON CONFLICT REPLACE

)`
	if _, err := db.Exec(stmt33); err != nil {
		return fmt.Errorf("failed to create lora combination random prompt images table: %v\n", err)
	}
	return nil
}

func queryAvailableLoraCombinationRandomPromptImages(appCtx AppContext, cid int) ([]string, []int, error) {
	stmt := `SELECT checkpointFilename, randomPromptListId FROM loraCombinationRandomPromptImagesV2 WHERE loraCombinationId = ?`
	rows, err := appCtx.dbr.Query(stmt, cid)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query random prompt images: %w", err)
	}

	checkpointFilenames := make([]string, 0, 10)
	randomPromptListIds := make([]int, 0, 10)
	for rows.Next() {
		var checkpointFilename string
		var randomPromptListId int
		if err := rows.Scan(&checkpointFilename, &randomPromptListId); err != nil {
			return nil, nil, fmt.Errorf("failed to scan query result: %w", err)
		}
		checkpointFilenames = append(checkpointFilenames, checkpointFilename)
		randomPromptListIds = append(randomPromptListIds, randomPromptListId)
	}

	return checkpointFilenames, randomPromptListIds, nil
}

func insertRandomPromptsV2(tx DB, prompts []string) (int, error) {
	result0, err := tx.Exec("INSERT INTO randomPromptListsV2 DEFAULT VALUES")
	if err != nil {
		return 0, fmt.Errorf("failed to insert random prompt list record: %w", err)
	}

	id, err := result0.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last inserted id: %w", err)
	}

	for index, prompt := range prompts {
		p0 := strings.ReplaceAll(prompt, "(", "\\(")
		p1 := strings.ReplaceAll(p0, ")", "\\)")
		p2 := strings.ReplaceAll(p1, ":", "\\:")
		stmt := "INSERT INTO randomPromptsV2 ( randomPromptListId, seq, prompt ) VALUES ( ?, ?, ? )"
		if _, err := tx.Exec(stmt, id, index+1, p2); err != nil {
			return 0, fmt.Errorf("failed to insert random prompt record: %w", err)
		}
	}

	return int(id), nil
}

func queryLoraCombinationIds(db DB) ([]int, error) {
	rows, err := db.Query("SELECT loraCombinationId FROM loraCombinationsV2")
	if err != nil {
		return nil, fmt.Errorf("failed to query lora combination ids: %w", err)
	}

	ids := make([]int, 0, 10)
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		ids = append(ids, id)
	}

	return ids, nil
}
