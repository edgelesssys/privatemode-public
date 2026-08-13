// Package model provides utilities for handling models.
package model

// Well-known model names.
const (
	OpenAIWhisperLargeV3 = "openai/whisper-large-v3"
	OpenAIGPTOSS120B     = "openai/gpt-oss-120b"
	KimiK26              = "kimi-k2.6"
	VoxtralMini3b        = "voxtral-mini-3b"
	DeepSeekOCR2         = "deepseek-ocr-2"
	GLM52                = "glm-5.2"
)

const (
	// TokensPerAudioSecondWhisper is the conversion rate from audio tokens to audio seconds for [OpenAIWhisperLargeV3].
	TokensPerAudioSecondWhisper = 100
	// TokensPerAudioSecondVoxtral is the conversion rate from audio tokens to audio seconds for voxtral.
	TokensPerAudioSecondVoxtral = 25
	// TokenOverheadVoxtral is the fixed amount of tokens added by the voxtral model for each request.
	TokenOverheadVoxtral = 323
)

// modelMap maps model aliases to their billing names.
var modelMap = map[string]string{
	"gpt-oss-latest":   OpenAIGPTOSS120B,
	"kimi-latest":      KimiK26,
	"glm-latest":       GLM52,
	"whisper-large-v3": OpenAIWhisperLargeV3,
	"gpt-oss-120b":     OpenAIGPTOSS120B,
}

// Resolve is used to ensure our code uses the full name of a model
// instead of pseudo names.
func Resolve(modelName string) string {
	// resolving model alias to DB IDs
	if fullName, ok := modelMap[modelName]; ok {
		return fullName
	}
	return modelName
}
