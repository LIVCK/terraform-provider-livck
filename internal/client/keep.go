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
