package client

import "encoding/json"

// KeepSentinel is what the API returns in place of every sealed secret value
// (HTTP header values, auth token/password/value). Sending it back verbatim
// keeps the stored secret unchanged (stable ciphertext, no worker resync).
const KeepSentinel = "__LIVCK_KEEP_UNCHANGED__"

// MergeSecrets walks the remote config and replaces every KeepSentinel leaf
// with the value at the same path from the prior (state) config, so the
// provider's Read never writes the sentinel into state and plans stay clean.
// Paths missing from the prior config keep the sentinel — the caller decides
// how to surface that (it only happens on import, where secrets are
// unknowable by design).
func MergeSecrets(remote, prior json.RawMessage) (json.RawMessage, error) {
	if len(remote) == 0 {
		return remote, nil
	}

	var remoteVal, priorVal any
	if err := json.Unmarshal(remote, &remoteVal); err != nil {
		return nil, err
	}
	if len(prior) > 0 {
		if err := json.Unmarshal(prior, &priorVal); err != nil {
			return nil, err
		}
	}

	merged := mergeValue(remoteVal, priorVal)

	out, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func mergeValue(remote, prior any) any {
	switch rv := remote.(type) {
	case string:
		if rv == KeepSentinel {
			if ps, ok := prior.(string); ok {
				return ps
			}
		}
		return rv
	case map[string]any:
		pm, _ := prior.(map[string]any)
		for k, v := range rv {
			rv[k] = mergeValue(v, pm[k])
		}
		return rv
	case []any:
		pa, _ := prior.([]any)
		for i, v := range rv {
			var p any
			if i < len(pa) {
				p = pa[i]
			}
			rv[i] = mergeValue(v, p)
		}
		return rv
	default:
		return remote
	}
}

// ReconcileConfig decides what the provider stores for settings.config.
//
// The API seeds check-type defaults (method, verify_ssl, default conditions …)
// into every persisted config, so the echo is a SUPERSET of what the user
// declared. As long as everything the user wrote still matches the remote
// value (after resolving keep-sentinels), the state keeps the user's exact
// JSON — server-seeded defaults stay unmanaged and plans stay clean. Only
// when a user-declared value actually changed remotely does the merged echo
// surface as drift.
//
// A null/empty prior means the user does not manage config at all — it stays
// null (the import case is documented: secrets and config must be re-declared).
func ReconcileConfig(remote, prior json.RawMessage) (json.RawMessage, error) {
	if len(prior) == 0 || string(prior) == "null" {
		return nil, nil
	}

	merged, err := MergeSecrets(remote, prior)
	if err != nil {
		return nil, err
	}

	var priorVal, mergedVal any
	if err := json.Unmarshal(prior, &priorVal); err != nil {
		return nil, err
	}
	if len(merged) == 0 {
		return merged, nil
	}
	if err := json.Unmarshal(merged, &mergedVal); err != nil {
		return nil, err
	}

	if deepSubset(priorVal, mergedVal) {
		return prior, nil
	}

	return merged, nil
}

// deepSubset reports whether every key/value declared in a exists with an
// equal value in b (recursively for objects; arrays and scalars compare
// exactly).
func deepSubset(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range av {
			if !deepSubset(v, bv[k]) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !deepSubset(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		aj, _ := json.Marshal(a)
		bj, _ := json.Marshal(b)
		return string(aj) == string(bj)
	}
}
