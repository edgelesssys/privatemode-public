package attestation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/edgelesssys/continuum/attestation-agent/internal/gpu/attestation/nras"
	"github.com/edgelesssys/continuum/attestation-agent/internal/gpu/attestation/policy"
	"github.com/golang-jwt/jwt/v5"
)

// A Verifier can verify attestation statements issued for local NVIDIA GPUs.
type Verifier struct {
	policy     *policy.NvidiaHopper
	log        *slog.Logger
	nrasClient nrasClient
}

// An nrasClient contains the methods needed by the verifier
// to communicate with the NRAS.
type nrasClient interface {
	JWKS(ctx context.Context) ([]byte, error)
}

// NewVerifier creates a new Verifier.
func NewVerifier(policy *policy.NvidiaHopper, logger *slog.Logger) *Verifier {
	return &Verifier{
		policy:     policy,
		log:        logger,
		nrasClient: nras.NewClient(logger),
	}
}

/*
Verify verifies the given EAT (Entity Attestation Token) with the public keys of the NRAS
against the given appraisal policy.

Verify will always verify:
  - That the token is signed with an expected algorithm.
  - That the signature is valid.
  - That the token is not expired.
  - That the token can be used already. (Not-Before)
  - That the token is issued by the NRAS.
  - That the token is issued as an subject for NVIDIA GPU attestation.
  - ~~That the token was issued for a HOPPER GPU.~~ Currently disabled, since EATs contain
    no audience (aud), as of `x-nvidia-eat-ver: EAT-21`.

The appraisal policy then is responsible for validating the claims of the token.
*/
func (v *Verifier) Verify(ctx context.Context, eat string, nonce [32]byte) error {
	v.log.Info("Verifying attestation token with NRAS")

	jwks, err := v.nrasClient.JWKS(ctx)
	if err != nil {
		return fmt.Errorf("retrieving JWKS: %w", err)
	}
	v.log.Info("Retrieved JWKS")

	// TODO: We could verify the certificate chain of the EAT against an embedded NVIDIA root certificate.
	// Probably, we also don't want to use a library to parse the keys.

	keyset, err := keyfunc.NewJWKSetJSON(jwks)
	if err != nil {
		return fmt.Errorf("creating keyfunc: %w", err)
	}

	token, err := jwt.Parse(eat, keyset.Keyfunc,
		// As of 2024-01-11, the only supported algorithm is ES384.
		// See https://docs.attestation.nvidia.com/api-docs/nras.html#get-/.well-known/jwks.json.
		jwt.WithValidMethods(
			[]string{"ES384"},
		),
		jwt.WithLeeway(2*time.Minute), // Allow 2 minutes of clock skew.
	)
	if err != nil {
		return fmt.Errorf("parsing EAT: %w", err)
	}

	if err := v.defaultClaimValidation(token); err != nil {
		return fmt.Errorf("validating EAT against default policy: %w", err)
	}

	if err := appraiseEAT(v.policy, token, nonce); err != nil {
		return fmt.Errorf("validating EAT against appraisal policy: %w", err)
	}

	v.log.Info("Validated EAT")
	return nil
}

/*
defaultClaimValidation validates the given token against the default claim validation policy.

For more information, see the documentation of `Verifier.Verify`.

(i.e. the policy that is always applied, regardless of the appraisal policy.)
*/
func (v *Verifier) defaultClaimValidation(token *jwt.Token) error {
	if !token.Valid {
		return fmt.Errorf("invalid EAT")
	}

	exp, err := token.Claims.GetExpirationTime()
	if err != nil {
		return fmt.Errorf("retrieving expiration time: %w", err)
	}
	if exp == nil {
		return fmt.Errorf("EAT has no expiration time")
	}
	if exp.Before(time.Now().Add(-2 * time.Minute)) { // Allow 2 minutes of clock skew.
		return fmt.Errorf("EAT expired at %s", exp)
	}

	nbf, err := token.Claims.GetNotBefore()
	if err != nil {
		return fmt.Errorf("retrieving not-before time: %w", err)
	}
	if nbf == nil {
		return fmt.Errorf("EAT has no not-before time")
	}
	if nbf.After(time.Now().Add(2 * time.Minute)) { // Allow 2 minutes of clock skew.
		return fmt.Errorf("EAT not valid before %s", nbf)
	}

	iss, err := token.Claims.GetIssuer()
	if err != nil {
		return fmt.Errorf("retrieving issuer: %w", err)
	}
	if iss == "" {
		return fmt.Errorf("EAT has no issuer")
	}
	if iss != nras.URL {
		return fmt.Errorf("EAT issued by %s, expected %s", iss, nras.URL)
	}

	sub, err := token.Claims.GetSubject()
	if err != nil {
		return fmt.Errorf("retrieving subject: %w", err)
	}
	if sub == "" {
		return fmt.Errorf("EAT has no subject")
	}
	if sub != nras.Subject {
		return fmt.Errorf("EAT issued for %s, expected %s", sub, nras.Subject)
	}

	// As of x-nvidia-eat-ver: EAT-21, EATs contain no audience.
	// Therefore, we temporarily disable this check.

	// aud, err := token.Claims.GetAudience()
	// if err != nil {
	// 	return fmt.Errorf("retrieving audience: %w", err)
	// }
	// if len(aud) < 1 {
	// 	return fmt.Errorf("EAT has no audience")
	// }
	// if aud[0] != string(nrasArchHopper) {
	// 	return fmt.Errorf("EAT issued for %s, expected %s", aud, nrasArchHopper)
	// }

	return nil
}
