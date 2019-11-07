# TSDB Gateway

This gateway provides access to the Hosted Metrics Graphite service at Grafana Labs, powered by [Metrictank](https://github.com/grafana/metrictank).

## tsdb-gw
  * Uses basic auth to verify requests.
  The username defaults to `api_key`, `username: api_key`
  The password is either a [grafana.com](grafana.com) api_key or a key located in the file auth, `password: <api_key>`
  Forwards metric and data requests to metrictank via a MetrictankProxy and a GraphiteProxy
  Handles ingestion with the corresponding plugin, but typical deployments all publish into Kafka.
  [Available http routes](./cmd/tsdb-gw/main.go)

  * [rate limiter](./documentation/ratelimiter.md)

## persister-gw

  [Available http routes](./cmd/persister-gw/main.go)
  TODO. @jtlisi

## Ingestion support

1. "metrics2.0" payloads in json or messagepack over http.
2. Carbon
3. Prometheus Remote Write
4. OpenTSDB HTTP write
5. DataDog JSON

## Authentication

Every request (whether for ingest, data, etc) is authenticated via [grafana.com](grafana.com) accounts/apikeys.

Plugins:
* FileAuth
* GrafanaComInstanceAuth
* GrafanaComAuth
* GCom

TODO @woodsaj describe the various plugins/methods, org vs instance, and the best practices, special admin keys, etc
TODO @woodsaj If auth works the same for the 3 services mentioned above, remove the auth stuff from their description

If you have more questions about authentication, ping @woodsaj and ask him to update this.
