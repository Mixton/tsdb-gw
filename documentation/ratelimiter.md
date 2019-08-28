# Rate limiter

The rate limiter implements a [token bucket](https://en.wikipedia.org/wiki/Token_bucket) to limit the amount of datapoints that can be ingested on a per-org level.
The configured limit is set both as a per-second rate limit, as well as the max burst size.
(this allows to burst the entire second budget in one single request, but blocks against requests that are too large and too infrequent)

We use a hybrid approach:
* any incoming request that would exhaust the secondly budget is immediately rejected with code 429.
  When this happens, it is up to the client to back off and retry (if bandwidth becomes an issue, in a future version it may be better to keep this request hanging and start reading when we're ready)
* other requests are decoded, checked and paused as necessary to honor the rate limit (but always proceed, even if the single request exceeds the budget. we don't block more granular than per-request)

