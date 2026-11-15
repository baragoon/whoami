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

| Flag      | Env var                    | Description                                                                                   |
|-----------|----------------------------|-----------------------------------------------------------------------------------------------|
| `cert`    | `WHOAMI_CERT`              | Give me a certificate.                                                                        |
| `key`     | `WHOAMI_KEY`               | Give me a key.                                                                                |
| `cacert`  | `WHOAMI_CACERT`            | Give me a CA chain, enforces mutual TLS                                                       |
| `port`    | `WHOAMI_PORT_NUMBER`       | Give me a port number. (default: `80`)                                                        |
| `name`    | `WHOAMI_NAME`              | Give me a name.                                                                               |
| `verbose` |                            | Enable verbose logging.                                                                       |
|           | `WHOAMI_HIDE_INTERNAL_IPS` | Hide loopback, private, link-local, and unspecified addresses when `true` (default: `false`). |
|           | `WHOAMI_HEALTHCHECK_HOST`  | Health check hostname (default: `127.0.0.1`); for TLS, must match the certificate SAN.        |

Docker images include a health check that runs `/whoami check` and checks the `/health` endpoint. It uses the `WHOAMI_*` settings above and requires no shell or external utilities in the image. When `WHOAMI_CERT` and `WHOAMI_KEY` configure HTTPS for the server, the check uses HTTPS and validates the server certificate using system roots unless `WHOAMI_HEALTHCHECK_SERVER_CA` is set. `WHOAMI_CACERT` remains the CA used by the server to verify client certificates; it is not used by the health check to verify the server.

Health-check client authentication is configured separately with `WHOAMI_HEALTHCHECK_CLIENT_CERT` and `WHOAMI_HEALTHCHECK_CLIENT_KEY`. Set both only when the server requires a client certificate; server-only certificates in `WHOAMI_CERT` and `WHOAMI_KEY` are not sent by the check.

The health check runs in a separate process and does not inherit command-line flags passed to the server. Use the `WHOAMI_*` environment variables to share configuration, or override the image's `HEALTHCHECK` and pass the same options to `check`, for example:

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
