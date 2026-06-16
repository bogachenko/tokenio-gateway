# 0002. Native API authentication carriers

## Status

Accepted

## Context

Tokenio Gateway exposes standard paths for OpenAI-compatible, Anthropic-native,
Gemini-native and Ollama-native clients.

The application authentication contract is one Tokenio user API key, but native
SDKs do not all transport API keys through the same HTTP header. Requiring one
wire-level carrier for every family would break drop-in path compatibility.
Allowing provider credentials or provider-specific authentication semantics
inside application code would mix trust boundaries.

## Decision

Authentication carrier selection belongs to the inbound transport adapter for
the detected API family.

Allowed first-version carriers:

```text
openai_compatible:
  Authorization: Bearer sk_...

anthropic_native:
  x-api-key: sk_...

gemini_native:
  x-goog-api-key: sk_...

ollama_native:
  Authorization: Bearer sk_...
```

Gemini query parameter authentication is not accepted:

```text
?key=sk_...
```

because URLs are more likely to be persisted in access logs, browser history,
proxies and tracing systems.

Each family transport adapter must:

```text
1. read only its explicitly allowed carrier;
2. validate carrier syntax without performing key lookup;
3. reject missing, malformed or ambiguous credentials;
4. normalize the value into one Tokenio raw API-key credential;
5. call the shared public authentication application use case;
6. remove the inbound Tokenio credential before upstream forwarding.
```

The application layer receives no carrier name and contains no family-specific
authentication branches.

If multiple supported credential carriers are ever added to one family, the
transport must reject conflicting values rather than choose one by precedence.

The normalized Tokenio key:

```text
is not a provider credential;
is not forwarded to Billing;
is not forwarded to a reseller;
is never logged;
is persisted only as the configured HMAC digest.
```

Ollama has no universal native authentication carrier. Tokenio therefore keeps
the standard Ollama paths but requires clients to support the explicit
`Authorization: Bearer sk_...` header when targeting Tokenio.

## Consequences

- Standard native paths remain unchanged.
- Authentication syntax is deterministic per API family.
- Generic application services authenticate one normalized Tokenio credential.
- Provider-specific upstream credentials remain resolved from
  `reseller.api_key_env`.
- Query-string user API keys are forbidden.
- Runtime support requires separate family transport adapters and acceptance
  tests; this ADR does not implement them.

## Rejected alternatives

### One Bearer carrier for every family

Rejected because Anthropic and Gemini SDKs normally expose native API-key
carriers and forcing a different carrier weakens drop-in compatibility.

### Accept every known provider carrier on every path

Rejected because it makes authentication ambiguous and leaks provider-specific
policy into the generic boundary.

### Accept Gemini `?key=...`

Rejected because secrets in URLs have an unnecessarily broad persistence and
logging surface.
