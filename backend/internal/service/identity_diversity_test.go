package service

import (
	"testing"
)

func TestGenerateDiverseFingerprint_Deterministic(t *testing.T) {
	// Same accountID must always produce the same fingerprint
	fp1 := generateDiverseFingerprint(12345)
	fp2 := generateDiverseFingerprint(12345)

	if fp1 != fp2 {
		t.Errorf("same accountID produced different fingerprints:\n  fp1=%+v\n  fp2=%+v", fp1, fp2)
	}

	// Verify multiple calls are stable
	for i := 0; i < 100; i++ {
		fp := generateDiverseFingerprint(12345)
		if fp != fp1 {
			t.Fatalf("fingerprint changed on iteration %d", i)
		}
	}
}

func TestGenerateDiverseFingerprint_Distribution(t *testing.T) {
	// Different accountIDs should produce varying fingerprints
	seen := make(map[string]bool)
	n := 200

	for i := int64(0); i < int64(n); i++ {
		fp := generateDiverseFingerprint(i)
		key := fp.StainlessOS + "|" + fp.StainlessArch + "|" + fp.StainlessRuntimeVersion + "|" + fp.StainlessPackageVersion + "|" + fp.UserAgent
		seen[key] = true
	}

	// With 200 accounts and pools of size 3*2*6*4*5 = 720 combinations,
	// we should see a reasonable number of distinct fingerprints
	if len(seen) < 10 {
		t.Errorf("poor distribution: only %d distinct fingerprints from %d accounts", len(seen), n)
	}

	// Verify we see multiple OS values
	osSet := make(map[string]bool)
	archSet := make(map[string]bool)
	for i := int64(0); i < int64(n); i++ {
		fp := generateDiverseFingerprint(i)
		osSet[fp.StainlessOS] = true
		archSet[fp.StainlessArch] = true
	}
	if len(osSet) < 2 {
		t.Errorf("expected multiple OS values, got %v", osSet)
	}
	if len(archSet) < 2 {
		t.Errorf("expected multiple Arch values, got %v", archSet)
	}
}

func TestGenerateDiverseFingerprint_ValidValues(t *testing.T) {
	validOS := map[string]bool{"Linux": true, "MacOS": true, "Windows": true}
	validArch := map[string]bool{"arm64": true, "x64": true}
	validRuntimeVer := map[string]bool{
		"v22.11.0": true, "v22.15.0": true, "v23.5.0": true,
		"v23.11.0": true, "v24.4.0": true, "v24.13.0": true,
	}
	validPkgVer := map[string]bool{"0.67.0": true, "0.68.0": true, "0.69.0": true, "0.70.0": true}
	validCLIVer := map[string]bool{
		"claude-cli/2.1.18 (external, cli)": true,
		"claude-cli/2.1.19 (external, cli)": true,
		"claude-cli/2.1.20 (external, cli)": true,
		"claude-cli/2.1.21 (external, cli)": true,
		"claude-cli/2.1.22 (external, cli)": true,
	}

	for i := int64(0); i < 500; i++ {
		fp := generateDiverseFingerprint(i)

		if !validOS[fp.StainlessOS] {
			t.Errorf("account %d: invalid OS %q", i, fp.StainlessOS)
		}
		if !validArch[fp.StainlessArch] {
			t.Errorf("account %d: invalid Arch %q", i, fp.StainlessArch)
		}
		if !validRuntimeVer[fp.StainlessRuntimeVersion] {
			t.Errorf("account %d: invalid RuntimeVersion %q", i, fp.StainlessRuntimeVersion)
		}
		if !validPkgVer[fp.StainlessPackageVersion] {
			t.Errorf("account %d: invalid PackageVersion %q", i, fp.StainlessPackageVersion)
		}
		if !validCLIVer[fp.UserAgent] {
			t.Errorf("account %d: invalid UserAgent %q", i, fp.UserAgent)
		}
		if fp.StainlessLang != "js" {
			t.Errorf("account %d: expected StainlessLang=js, got %q", i, fp.StainlessLang)
		}
		if fp.StainlessRuntime != "node" {
			t.Errorf("account %d: expected StainlessRuntime=node, got %q", i, fp.StainlessRuntime)
		}
	}
}
