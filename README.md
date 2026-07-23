# device-ingress

`device-ingress` accepts OpenRTB 2.x HTTP bid requests and writes normalized device records to Queue Bridge topic `device`. It is an ingestion adapter only. Existing workers retain the rest of the flow:

```text
HTTP OpenRTB -> device-ingress -> queue-bridge topic=device
            -> device-filter -> topic=device-8
            -> ddj-work -> topic=ddj -> do-click
```

## HTTP API

`POST /v1/openrtb/device`

The request body is a single OpenRTB JSON BidRequest. `Content-Type: application/json` and `application/openrtb+json` are accepted. Gzip request bodies are supported with `Content-Encoding: gzip`; a gzip body without that header is also detected from its `1f 8b` magic bytes for upstream compatibility.

Every request returns `204 No Content` with an empty body. Invalid JSON, invalid device fields, missing Android IFA, a full ingress queue, and asynchronous Queue Bridge delivery failures are exposed through logs and `/metrics`, not the HTTP response.

### Required usable fields

- `app.bundle` or `app.id`
- `imp[0]`
- `device.os` (`android` or `ios`)
- `device.ip`
- `device.ua`
- Android only: `device.ifa`, matching the v4 UUID check already enforced by `device-filter`

The supplied upstream example is structurally OpenRTB 2.5, but has no `device.ifa`. It will return `204` and increment `device_ingress_invalid_android_ifa_total`; it is intentionally not sent to the `device` queue because the existing device filter would drop it as well.

## OpenRTB mapping

| OpenRTB field | device-filter field |
| --- | --- |
| `device.os` | `os` |
| `device.ip` | `ip` |
| `device.ua` | `ua` |
| `device.ifa` | `ifa` |
| `device.geo.country/city/region` | `country/city/state` |
| `device.make/model/osv/carrier/connectiontype` | `brand/device/os_version/network_carrier/network_access` |
| `app.bundle` (fallback `app.id`) | `app_id` |
| `id` | `ad_request_id` |
| `source.tid` | `session_id` |
| `imp[0].id/tagid/banner.w/banner.h` | `site_id/ad_placement/ad_width/ad_height` |
| `clientId` (fallback `supplyId`) | `adx_id` |
| `supplyId` | `media_source` |

`clientId` and `supplyId` are upstream extension fields, not standard OpenRTB fields. Unknown standard and `ext` fields are intentionally ignored.

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `LISTEN_ADDR` | `:8080` | HTTP listener |
| `QUEUE_BRIDGE_ADDR` | `queue-bridge:50051` | Queue Bridge gRPC endpoint |
| `QUEUE_TOPIC` | `device` | Queue Bridge topic |
| `MAX_BODY_BYTES` | `1048576` | Compressed and decoded request-body limit |
| `QUEUE_DEPTH` | `32768` | Total in-memory admission queue depth |
| `BATCH_SIZE` | `500` | Messages per Queue Bridge `PushBatch` |
| `BATCH_WAIT` | `2ms` | Maximum batch wait |
| `PUSH_WORKERS` | `4` | Concurrent batch publishers |
| `PUSH_TIMEOUT` | `2s` | Queue Bridge gRPC timeout per attempt |
| `PUSH_RETRIES` | `2` | Retries after the initial push attempt |

The HTTP `204` contract means accepted data is only memory-buffered at response time. Queue saturation or persistent Queue Bridge failures cause drops, which must be alerted from `device_ingress_dropped_queue_full_total` and `device_ingress_queue_push_failures_total`.

## Development

```bash
go test ./... -count=1
go test ./internal/httpapi -run '^$' -bench BenchmarkIngestOpenRTB -benchtime=3s
docker build --platform linux/amd64 -t device-ingress:local .
```
