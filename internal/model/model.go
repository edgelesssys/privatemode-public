// Package model provides utilities for handling models.
package model

// Well-known model names.
const (
	OpenAIWhisperLargeV3 = "openai/whisper-large-v3"
	OpenAIGPTOSS120B     = "openai/gpt-oss-120b"
	KimiLatest           = "kimi-k2.6"
	VoxtralMini3b        = "voxtral-mini-3b"
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
	"whisper-large-v3": OpenAIWhisperLargeV3,
	"gpt-oss-120b":     OpenAIGPTOSS120B,
	"kimi-latest":      KimiLatest,
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
