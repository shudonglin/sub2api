package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const attributionFingerprintSalt = "59cf53e54c78"

// computeAttributionFingerprint computes the 3-char hex fingerprint that Claude Code
// includes in its attribution billing header. The algorithm is:
//
//	SHA256(SALT + msgText[4] + msgText[7] + msgText[20] + version)[:3]
//
// where out-of-bounds character indices are replaced with "0".
func computeAttributionFingerprint(firstUserMsgText string, version string) string {
	charAtOrZero := func(s string, i int) string {
		if i < len(s) {
			return string(s[i])
		}
		return "0"
	}
	input := attributionFingerprintSalt +
		charAtOrZero(firstUserMsgText, 4) +
		charAtOrZero(firstUserMsgText, 7) +
		charAtOrZero(firstUserMsgText, 20) +
		version
	hash := sha256.Sum256([]byte(input))
	return hex.EncodeToString(hash[:])[:3]
}

// extractFirstUserMessageText walks the messages array and returns the text
// content of the first message with role "user". It handles both string content
// and array-of-blocks content formats.
func extractFirstUserMessageText(messages []any) string {
	for _, msg := range messages {
		m, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		if m["role"] != "user" {
			continue
		}
		content := m["content"]
		switch c := content.(type) {
		case string:
			return c
		case []any:
			for _, block := range c {
				b, ok := block.(map[string]any)
				if !ok {
					continue
				}
				if b["type"] == "text" {
					if text, ok := b["text"].(string); ok {
						return text
					}
				}
			}
		}
		return ""
	}
	return ""
}

// buildAttributionHeader returns the formatted attribution billing header string.
func buildAttributionHeader(version string, fingerprint string) string {
	return fmt.Sprintf("x-anthropic-billing-header: cc_version=%s.%s; cc_entrypoint=cli;", version, fingerprint)
}

// injectAttributionBlock prepends an attribution billing header as the first
// system text block in the request body. It handles null, string, and array
// system formats.
func injectAttributionBlock(body []byte, headerText string) []byte {
	attrBlock, err := marshalAnthropicSystemTextBlock(headerText, true)
	if err != nil {
		return body
	}

	sysResult := gjson.GetBytes(body, "system")

	var items [][]byte

	switch {
	case !sysResult.Exists() || sysResult.Type == gjson.Null:
		items = [][]byte{attrBlock}
	case sysResult.Type == gjson.String:
		existingBlock, err := marshalAnthropicSystemTextBlock(sysResult.String(), false)
		if err != nil {
			return body
		}
		items = [][]byte{attrBlock, existingBlock}
	case sysResult.IsArray():
		items = make([][]byte, 0, len(sysResult.Array())+1)
		items = append(items, attrBlock)
		sysResult.ForEach(func(_, item gjson.Result) bool {
			items = append(items, []byte(item.Raw))
			return true
		})
	default:
		return body
	}

	newSystem := buildJSONArrayRaw(items)
	result, err := sjson.SetRawBytes(body, "system", newSystem)
	if err != nil {
		return body
	}
	return result
}
