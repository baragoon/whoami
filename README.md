# whoami

[![Build Status](https://github.com/baragoon/whoami/workflows/Main/badge.svg?branch=master)](https://github.com/baragoon/whoami/actions)

Tiny Go webserver that prints OS information and HTTP request to output.

## Usage

### Paths

#### `/[?wait=d]`

Returns the whoami information (request and network information).

The optional `wait` query parameter can be provided to tell the server to wait before sending the response.
The duration is expected in Go's [`time.Duration`](https://golang.org/pkg/time/#ParseDuration) format (e.g. `/?wait=100ms` to wait 100 milliseconds).

The optional `env` query parameter can be set to `true` to add the environment variables to the response.

#### `/api`

Returns the whoami information (and some extra information) as JSON.

The optional `env` query parameter can be set to `true` to add the environment variables to the response.

#### `/bench`

Always return the same response (`1`).

#### `/data?size=n[&unit=u]`

Creates a response with a size `n`.

The unit of measure, if specified, accepts the following values: `KB`, `MB`, `GB`, `TB` (optional, default: bytes).

#### `/echo`

WebSocket echo.

#### `/health`

Heath check.

- `GET`, `HEAD`, ...: returns a response with the status code defined by the `POST`
- `POST`: changes the status code of the `GET` (`HEAD`, ...) response.

### Flags

| Flag                                  | Env var                              | Description                                                                                   |
|---------------------------------------|--------------------------------------|-----------------------------------------------------------------------------------------------|
| `--cert`                              | `WHOAMI_CERT`                        | Give me a certificate.                                                                        |
| `--key`                               | `WHOAMI_KEY`                         | Give me a key.                                                                                |
| `--cacert` / `check --cacert`         | `WHOAMI_CACERT`                      | Server client CA; for `check`, indicates that the server requires a client certificate.        |
| `--port`                              | `WHOAMI_PORT_NUMBER`                 | Give me a port number. (default: `80`)                                                        |
| `--name`                              | `WHOAMI_NAME`                        | Give me a name.                                                                               |
| `--verbose`                           | `WHOAMI_VERBOSE`                     | Enable verbose logging.                                                                       |
| `--hide-internal-ips`                 | `WHOAMI_HIDE_INTERNAL_IPS`           | Hide loopback, private, link-local, and unspecified addresses (default: `false`).             |
| `--hide-x-forwarded-server`           | `WHOAMI_HIDE_X_FORWARDED_SERVER`     | Hide the `X-Forwarded-Server` request header (default: `false`).                              |
| `check --healthcheck-host`            | `WHOAMI_HEALTHCHECK_HOST`            | Health check hostname (default: `127.0.0.1`); for TLS, must match the certificate SAN.        |
| `check --healthcheck-client-cert`     | `WHOAMI_HEALTHCHECK_CLIENT_CERT`     | Client certificate for health checks.                                                         |
| `check --healthcheck-client-key`      | `WHOAMI_HEALTHCHECK_CLIENT_KEY`      | Client key for health checks.                                                                 |
| `check --healthcheck-server-ca`       | `WHOAMI_HEALTHCHECK_SERVER_CA`       | CA certificate used to validate the health-check server.                                     |

Docker images enable a health check that runs `/whoami check` against `/health`; it requires no shell or external utilities. The check runs in a separate process and does not receive command-line arguments passed to the server. If the server uses CLI-only settings such as `--port`, `--cert`, `--key`, or `--cacert`, the health check will use its defaults unless you pass matching options to `check` in an overridden `HEALTHCHECK`. Alternatively, use the corresponding `WHOAMI_*` environment variables so the server and check share the settings. Otherwise, a working custom-port or TLS service may be reported unhealthy.

When `WHOAMI_CERT` and `WHOAMI_KEY` configure HTTPS for the server, the check uses HTTPS and validates the server certificate using system roots unless `WHOAMI_HEALTHCHECK_SERVER_CA` or `check --healthcheck-server-ca` is set. `WHOAMI_CACERT` configures the server's client-certificate verification; `check --cacert` tells the check that the server requires a client certificate, but neither setting is used to verify the server.

Health-check client authentication is configured separately with `WHOAMI_HEALTHCHECK_CLIENT_CERT` and `WHOAMI_HEALTHCHECK_CLIENT_KEY`, or the matching `check --healthcheck-client-cert` and `check --healthcheck-client-key` flags. Set both when the server requires a client certificate; server-only certificates in `WHOAMI_CERT` and `WHOAMI_KEY` are not sent by the check. **Existing mTLS deployments must configure these client credentials (and `WHOAMI_HEALTHCHECK_SERVER_CA` when needed) before using the image's built-in health check.** Otherwise the check fails and Docker marks the container unhealthy. Until migrated, disable the image check with `docker run --no-healthcheck` or override it with an appropriately configured health command.

To override the image's `HEALTHCHECK` and pass matching options to `check`, for example:

```dockerfile
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD ["/whoami", "check", "--port=8443", "--cert=/certs/server.crt", "--key=/certs/server.key", "--healthcheck-client-cert=/certs/client.crt", "--healthcheck-client-key=/certs/client.key", "--healthcheck-server-ca=/certs/server-ca.crt", "--healthcheck-host=whoami.example"]
```

## Examples

```console
$ docker run -d -p 8080:80 --name iamfoo ghcr.io/baragoon/whoami

$ curl http://localhost:8080
Hostname: 9c9c93da54b5
IP: 127.0.0.1
IP: ::1
IP: 172.17.0.2
RemoteAddr: 172.17.0.1:41040
GET / HTTP/1.1
Host: localhost:8080
User-Agent: curl/8.5.0
Accept: */*
```

```console
# updates health check status
$ curl -X POST -d '500' http://localhost:8080/health

# calls the health check
$ curl -v http://localhost:8080/health
* Host localhost:8080 was resolved.
* IPv6: ::1
* IPv4: 127.0.0.1
*   Trying [::1]:8080...
* Connected to localhost (::1) port 8080
> GET /health HTTP/1.1
> Host: localhost:8080
> User-Agent: curl/8.5.0
> Accept: */*
> 
< HTTP/1.1 500 Internal Server Error
< Date: Fri, 18 Apr 2025 13:36:02 GMT
< Content-Length: 0
```

```console
$ openssl req -newkey rsa:4096 \
    -x509 \
    -sha256 \
    -days 3650 \
    -nodes \
    -out ./certs/example.crt \
    -keyout ./certs/example.key

$ docker run -d -p 8080:80 -v ./certs:/certs --name iamfoo ghcr.io/baragoon/whoami --cert /certs/example.crt --key /certs/example.key

$ curl https://localhost:8080 -k --cert certs/example.crt  --key certs/example.key
Hostname: 25bc0df47b95
IP: 127.0.0.1
IP: ::1
IP: 172.17.0.2
RemoteAddr: 172.17.0.1:50278
Certificate[0] Subject: CN=traefik.io,O=TraefikLabs,L=Lyon,ST=France,C=FR
GET / HTTP/1.1
Host: localhost:8080
User-Agent: curl/8.5.0
Accept: */*
```

```console
$ docker run -d -p 8080:80 --name iamfoo ghcr.io/baragoon/whoami

$ grpcurl -plaintext -proto grpc.proto localhost:8080 whoami.Whoami/Whoami
{
  "hostname": "5a45e21984b4",
  "iface": [
    "127.0.0.1",
    "::1",
    "172.17.0.2"
  ]
}

$ grpcurl -plaintext -proto grpc.proto localhost:8080 whoami.Whoami/Bench
{
  "data": 1
}
```

```yml
version: '3.9'

services:
  whoami:
    image: ghcr.io/baragoon/whoami
    command:
       # It tells whoami to start listening on 2001 instead of 80
       - --port=2001
       - --name=iamfoo
```
