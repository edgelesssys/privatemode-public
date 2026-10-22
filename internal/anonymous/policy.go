// Package anonymous defines the anonymous chat policy shared by the app and proxy.
package anonymous

import (
	_ "embed"
	"encoding/json"
)

//go:embed policy.json
var policyJSON []byte

type chatPolicy struct {
	MaxCharacters int               `json:"maxCharacters"`
	Models        map[string]string `json:"models"`
	SystemPrompts map[string]string `json:"systemPrompts"`
}

// Policy is shared with the chat app. Treat it as read-only. Deploy new prompts
// and models to the inference proxy before deploying the corresponding app.
var Policy = func() chatPolicy {
	var policy chatPolicy
	if err := json.Unmarshal(policyJSON, &policy); err != nil {
		panic(err)
	}
	return policy
}()
