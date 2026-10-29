//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/privatemode"
	"github.com/stretchr/testify/require"
)

func TestNvidiaOCSPPolicyFromArg(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   string
		want    privatemode.NvidiaOCSPPolicy
		wantErr string
	}{
		{name: "defaults", input: `{}`},
		{
			name:  "fractional hours",
			input: `{"allowUnknown": true, "revokedGracePeriodHours": 1.5}`,
			want:  privatemode.NvidiaOCSPPolicy{AllowUnknown: true, RevokedGracePeriod: 90 * time.Minute},
		},
		{name: "zero", input: `{"allowUnknown": false, "revokedGracePeriodHours": 0}`},
		{name: "null", input: `null`},
		{name: "array", input: `[]`, wantErr: "decoding NVIDIA OCSP policy"},
		{name: "malformed JSON", input: `{`, wantErr: "decoding NVIDIA OCSP policy"},
		{
			name:  "null boolean",
			input: `{"allowUnknown": null, "revokedGracePeriodHours": 1.5}`,
			want:  privatemode.NvidiaOCSPPolicy{RevokedGracePeriod: 90 * time.Minute},
		},
		{name: "string boolean", input: `{"allowUnknown": "true"}`, wantErr: "allowUnknown"},
		{
			name:  "null duration",
			input: `{"allowUnknown": true, "revokedGracePeriodHours": null}`,
			want:  privatemode.NvidiaOCSPPolicy{AllowUnknown: true},
		},
		{name: "string duration", input: `{"revokedGracePeriodHours": "48"}`, wantErr: "revokedGracePeriodHours"},
		{name: "negative duration", input: `{"revokedGracePeriodHours": -1}`, wantErr: "must be nonnegative"},
		{name: "overflow", input: `{"revokedGracePeriodHours": 2562048}`, wantErr: "fit in a Go duration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := js.ValueOf(tc.input)
			got, err := nvidiaOCSPPolicyFromArg([]js.Value{js.Undefined(), value}, 1)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
	got, err := nvidiaOCSPPolicyFromArg(nil, 0)
	require.NoError(t, err)
	require.Equal(t, privatemode.NvidiaOCSPPolicy{}, got)
	got, err = nvidiaOCSPPolicyFromArg([]js.Value{js.Undefined()}, 0)
	require.NoError(t, err)
	require.Equal(t, privatemode.NvidiaOCSPPolicy{}, got)
	_, err = nvidiaOCSPPolicyFromArg([]js.Value{js.ValueOf(map[string]any{})}, 0)
	require.ErrorContains(t, err, "must be a JSON string")
}
