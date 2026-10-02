// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"path/filepath"
	"testing"
	"unicode"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlagAliases(t *testing.T) {
	// These tests are sequential because New binds flags to package variables.
	for _, tc := range []struct {
		oldName string
		name    string
		value   string
		target  any
		want    any
	}{
		{"apiKey", "api-key", "test-key", &apiKeyStr, "test-key"},
		{"apiEndpoint", "api-endpoint", "api.example:8443", &apiEndpoint, "api.example:8443"},
		{"corsAllowedOrigins", "cors-allowed-origins", "https://one.example,https://two.example", &corsAllowedOrigins, []string{"https://one.example", "https://two.example"}},
		{"manifestPath", "manifest-path", "/manifest.json", &manifestPath, "/manifest.json"},
		{"nvidiaOCSPAllowUnknown", "nvidia-ocsp-allow-unknown", "false", &nvidiaOCSPAllowUnknown, false},
		{"nvidiaOCSPRevokedGracePeriod", "nvidia-ocsp-revoked-grace-period", "7", &nvidiaOCSPRevokedGracePeriod, 7},
		{"insecureAPIConnection", "insecure-api-connection", "true", &insecureAPIConnection, true},
		{"tlsCertPath", "tls-cert-path", "/cert.pem", &tlsCertPath, "/cert.pem"},
		{"tlsKeyPath", "tls-key-path", "/key.pem", &tlsKeyPath, "/key.pem"},
		{"cdnBaseURL", "cdn-base-url", "https://cdn.example", &cdnBaseURL, "https://cdn.example"},
		{"dumpRequests", "dump-requests", "true", &dumpRequests, true},
	} {
		for _, name := range []string{tc.name, tc.oldName} {
			t.Run(name, func(t *testing.T) {
				cmd := New()
				var output bytes.Buffer
				cmd.SetOut(&output)
				cmd.SetErr(&output)
				require.NoError(t, cmd.ParseFlags([]string{"--" + name + "=" + tc.value}))
				assert.EqualValues(t, tc.want, dereferenceFlagTarget(tc.target))
				assert.True(t, cmd.Flags().Changed(name))
				if name == tc.oldName {
					assert.Contains(t, output.String(), "Flag --"+name+" has been deprecated, use --"+tc.name+" instead")
				} else {
					assert.Empty(t, output.String())
				}
			})
		}
	}
}

func dereferenceFlagTarget(target any) any {
	switch target := target.(type) {
	case *string:
		return *target
	case *bool:
		return *target
	case *int:
		return *target
	case *[]string:
		return *target
	default:
		panic("unsupported flag target")
	}
}

func TestMixedFlagAliases(t *testing.T) {
	for _, oldFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "new first", true: "old first"}[oldFirst], func(t *testing.T) {
			cmd := New()
			first, second := "api-key", "apiKey"
			originsFirst, originsSecond := "cors-allowed-origins", "corsAllowedOrigins"
			if oldFirst {
				first, second = second, first
				originsFirst, originsSecond = originsSecond, originsFirst
			}
			require.NoError(t, cmd.ParseFlags([]string{
				"--" + first, "first-key", "--" + second + "=last-key",
				"--" + originsFirst + "=https://one.example,https://two.example",
				"--" + originsSecond + "=https://three.example",
				"--dumpRequests", "--dump-requests=false",
				"--insecure-api-connection", "--insecureAPIConnection=false",
			}))
			assert.Equal(t, "last-key", apiKeyStr)
			assert.Equal(t, []string{"https://one.example", "https://two.example", "https://three.example"}, corsAllowedOrigins)
			assert.False(t, dumpRequests)
			assert.False(t, insecureAPIConnection)
		})
	}
}

func TestFlagHelp(t *testing.T) {
	cmd := New()
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		if flag.Deprecated != "" {
			assert.True(t, flag.Hidden, flag.Name)
			return
		}
		for _, r := range flag.Name {
			assert.False(t, unicode.IsUpper(r), flag.Name)
		}
	})
	for _, name := range []string{"insecure-api-connection", "cdn-base-url"} {
		assert.True(t, cmd.Flags().Lookup(name).Hidden, name)
	}
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--help"})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, output.String(), "--api-key string")
	assert.NotContains(t, output.String(), "--apiKey")
	assert.NotContains(t, output.String(), "deprecated")
}

func TestAPIKeyFileAliases(t *testing.T) {
	for _, name := range []string{"api-key", "apiKey"} {
		t.Run(name, func(t *testing.T) {
			cmd := New()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			// Stop before network setup, proving runProxy recognizes either spelling
			// as an explicitly supplied API key and tries to read the file.
			cmd.SetArgs([]string{"--" + name + "=@" + filepath.Join(t.TempDir(), "missing")})
			require.ErrorContains(t, cmd.Execute(), "failed to read API key file")
			assert.NotContains(t, output.String(), "No API key provided")
		})
	}
}
