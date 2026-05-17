package service

import "testing"

import "github.com/stretchr/testify/require"

// TestSanitizeGroupMessagesDispatchFields_DerivedSet verifies the gate fix
// from design Change 8c: the sanitize function preserves dispatch config when
// the group has at least one OpenAI account in its derived AccountPlatforms
// set, regardless of g.Platform. Wipes dispatch config only when the derived
// set has no OpenAI accounts.
func TestSanitizeGroupMessagesDispatchFields_DerivedSet(t *testing.T) {
	dispatchCfg := OpenAIMessagesDispatchModelConfig{
		OpusMappedModel:   "gpt-5.4",
		SonnetMappedModel: "gpt-5.3-codex",
		HaikuMappedModel:  "gpt-5.4-mini",
	}

	t.Run("anthropic_primary_with_openai_account_preserves", func(t *testing.T) {
		g := &Group{
			Platform:                    PlatformAnthropic,
			AccountPlatforms:            []string{PlatformAnthropic, PlatformOpenAI},
			AllowMessagesDispatch:       true,
			DefaultMappedModel:          "gpt-5.4",
			MessagesDispatchModelConfig: dispatchCfg,
		}
		sanitizeGroupMessagesDispatchFields(g)
		require.True(t, g.AllowMessagesDispatch, "dispatch flag must be preserved")
		require.Equal(t, "gpt-5.4", g.DefaultMappedModel)
		require.Equal(t, dispatchCfg, g.MessagesDispatchModelConfig)
	})

	t.Run("anthropic_primary_only_anthropic_accounts_wipes", func(t *testing.T) {
		g := &Group{
			Platform:                    PlatformAnthropic,
			AccountPlatforms:            []string{PlatformAnthropic},
			AllowMessagesDispatch:       true,
			DefaultMappedModel:          "gpt-5.4",
			MessagesDispatchModelConfig: dispatchCfg,
		}
		sanitizeGroupMessagesDispatchFields(g)
		require.False(t, g.AllowMessagesDispatch, "dispatch flag must be wiped")
		require.Empty(t, g.DefaultMappedModel)
		require.Equal(t, OpenAIMessagesDispatchModelConfig{}, g.MessagesDispatchModelConfig)
	})

	t.Run("openai_primary_preserves_legacy_behavior", func(t *testing.T) {
		// Single-platform regression: openai-primary, AccountPlatforms nil
		// (legacy snapshot) -> derived set falls back to [openai] -> preserve.
		g := &Group{
			Platform:                    PlatformOpenAI,
			AllowMessagesDispatch:       true,
			DefaultMappedModel:          "gpt-5.4",
			MessagesDispatchModelConfig: dispatchCfg,
		}
		sanitizeGroupMessagesDispatchFields(g)
		require.True(t, g.AllowMessagesDispatch)
		require.Equal(t, "gpt-5.4", g.DefaultMappedModel)
		require.Equal(t, dispatchCfg, g.MessagesDispatchModelConfig)
	})

	t.Run("gemini_primary_no_openai_accounts_wipes", func(t *testing.T) {
		// Single-platform regression: gemini-primary, no openai accounts -> wipe.
		g := &Group{
			Platform:                    PlatformGemini,
			AccountPlatforms:            []string{PlatformGemini},
			AllowMessagesDispatch:       true,
			DefaultMappedModel:          "gpt-5.4",
			MessagesDispatchModelConfig: dispatchCfg,
		}
		sanitizeGroupMessagesDispatchFields(g)
		require.False(t, g.AllowMessagesDispatch)
		require.Empty(t, g.DefaultMappedModel)
		require.Equal(t, OpenAIMessagesDispatchModelConfig{}, g.MessagesDispatchModelConfig)
	})

	t.Run("nil_group_safe", func(t *testing.T) {
		require.NotPanics(t, func() {
			sanitizeGroupMessagesDispatchFields(nil)
		})
	})
}

func TestNormalizeOpenAIMessagesDispatchModelConfig(t *testing.T) {
	t.Parallel()

	cfg := normalizeOpenAIMessagesDispatchModelConfig(OpenAIMessagesDispatchModelConfig{
		OpusMappedModel:   " gpt-5.4-high ",
		SonnetMappedModel: "gpt-5.3-codex",
		HaikuMappedModel:  " gpt-5.4-mini-medium ",
		ExactModelMappings: map[string]string{
			" claude-sonnet-4-5-20250929 ": " gpt-5.2-high ",
			"":                             "gpt-5.4",
			"claude-opus-4-6":              " ",
		},
	})

	require.Equal(t, "gpt-5.4", cfg.OpusMappedModel)
	require.Equal(t, "gpt-5.3-codex", cfg.SonnetMappedModel)
	require.Equal(t, "gpt-5.4-mini", cfg.HaikuMappedModel)
	require.Equal(t, map[string]string{
		"claude-sonnet-4-5-20250929": "gpt-5.2",
	}, cfg.ExactModelMappings)
}
