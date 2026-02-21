package main

import (
	_ "embed"
)

//go:embed html/submit-lora-combination-v2.html
var submitLoraCombinationHtmlV2 string

//go:embed html/submit-lora.html
var submitLoraHtml string

//go:embed html/submit-checkpoint.html
var submitCheckpointHtml string

//go:embed html/index.html
var indexHtml string

//go:embed html/browse-lora.html
var browseLoraHtml string

//go:embed html/browse-lora-v2.html
var browseLoraV2Html string

//go:embed html/browse-combination.html
var browseCombinationHtml string

//go:embed html/browse-combination-v2.html
var browseCombinationV2Html string

//go:embed html/lora.html
var loraHtml string

//go:embed html/lora-v2.html
var loraHtmlV2 string

//go:embed html/combination.html
var combinationHtml string

//go:embed html/combination-v2.html
var combinationV2Html string

//go:embed html/test-combination.html
var testCombinationHtml string

//go:embed html/submit-lora-v2.html
var submitLoraV2Html string

//go:embed js/submit-lora-v2.js
var submitLoraV2Js string
