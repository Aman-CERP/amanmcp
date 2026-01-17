# Vector Search Overview

What vector search is, why it matters, and when to use it.

**Reading time:** 3 minutes
**Audience:** Everyone
**Prerequisites:** None (but [Search Fundamentals](../../learning/search-fundamentals.md) provides helpful background)

---

## Quick Summary

- Vector search finds content by **meaning**, not just keywords
- Text is converted to numbers (embeddings) that capture meaning
- Similar meanings → similar numbers → found together in search
- Essential for natural language queries and concept discovery

---

## The Problem with Keyword Search

Traditional search (grep, BM25) finds exact words:

```
Search: "authentication"

✓ Finds: "authentication middleware"
✗ Misses: "login credentials"     (different words, same concept)
✗ Misses: "verify user identity"  (different words, same concept)
```

You have to guess which exact words the code uses. If you search "auth" but the code says "credentials", you miss it.

---

## The Solution: Search by Meaning

Vector search converts text into numbers that represent **meaning**:

```
"authentication"     →  [0.8, 0.3, 0.9, ...]
"login credentials"  →  [0.8, 0.3, 0.8, ...]  ← Similar numbers!
"weather forecast"   →  [0.1, 0.7, 0.2, ...]  ← Different numbers

Similar meaning = Similar numbers = Found together
```

When you search "authentication", vector search also finds "login", "credentials", "verify user" - because they have similar meanings.

---

## Think of It Like GPS Coordinates

Imagine meanings as locations on a map:

```
                    ┌─────────────────────────────────┐
                    │                                 │
                    │    "login" ●  ● "auth"          │
                    │              ● "credentials"    │
  Security          │                                 │
  District          │                                 │
                    │                                 │
                    └─────────────────────────────────┘

                    ┌─────────────────────────────────┐
                    │                                 │
                    │    "rain" ●                     │
  Weather           │           ● "forecast"         │
  District          │    "sunny" ●                   │
                    │                                 │
                    └─────────────────────────────────┘
```

- "login", "auth", and "credentials" are close together (same neighborhood)
- "rain" and "forecast" are close together (different neighborhood)
- Security and Weather concepts are far apart

When you search for "authentication", vector search finds everything in the "Security District" - even if they use different words.

---

## Why This Matters for Code Search

| Scenario | Keyword Search | Vector Search |
|----------|----------------|---------------|
| Search "authentication" | Only exact matches | Also finds "login", "verify" |
| Search "error handling" | Only "error" matches | Also finds "panic recovery" |
| Search "how does caching work" | Fails (too vague) | Finds cache implementations |

Vector search understands **what you mean**, not just what you type.

---

## How AmanMCP Uses Vector Search

1. **Index time:** Every code chunk gets converted to an embedding (list of numbers)
2. **Search time:** Your query gets converted to an embedding
3. **Match:** Find chunks whose embeddings are similar to your query
4. **Combine:** Results merge with keyword search via [hybrid search](../hybrid-search/)

All of this runs **locally** - your code never leaves your machine.

---

## When Vector Search Shines

| Query Type | Why Vector Works |
|------------|------------------|
| Natural language | "how does auth work" |
| Concept exploration | "error handling patterns" |
| Synonym handling | "cancel" finds "abort" |
| Fuzzy intent | "something with payments" |

## When to Use Keyword Search Instead

| Query Type | Why Keywords Work Better |
|------------|-------------------------|
| Exact function names | `ValidateToken` |
| Error codes | `ERR_AUTH_001` |
| Specific identifiers | `processPaymentV2` |

**AmanMCP uses both** via hybrid search - you get the best of both worlds.

---

## The "768 Dimensions" Thing

You might hear that embeddings have "768 dimensions". What does that mean?

**Simple answer:** 768 different aspects of meaning, like:
- Is it technical or everyday language?
- Is it about security or data?
- Is it positive or negative?
- ...and 765 more aspects

The model learns these aspects automatically from billions of examples. You don't need to understand them - just know that more dimensions = more nuance captured.

---

## Next Steps

| Want to... | Read |
|------------|------|
| Understand embeddings and similarity | [How It Works](how-it-works.md) |
| Learn HNSW and implementation details | [Advanced](advanced.md) |
| See how vector + keyword combine | [Hybrid Search](../hybrid-search/) |
| Try searching | [Your First Search](../../tutorials/your-first-search.md) |

---

*Vector search finds meaning, not just words. It's how AI understands your intent.*
