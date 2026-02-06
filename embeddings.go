package main

import (
	_ "embed"
)

//go:embed submit-lora-combination.html
var submitLoraCombinationHtml string

//go:embed submit-lora.html
var submitLoraHtml string

//go:embed submit-checkpoint.html
var submitCheckpointHtml string

//go:embed index.html
var indexHtml string

//go:embed browse-lora.html
var browseLoraHtml string

//go:embed browse-combination.html
var browseCombinationHtml string

//go:embed lora.html
var loraHtml string

//go:embed combination.html
var combinationHtml string

//go:embed test-combination.html
var testCombinationHtml string
