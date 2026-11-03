![httptoy — Break the happy path.](frontend/public/assets/social.png)

<p align="center">
  <strong>A playground for the beautifully broken side of HTTP.</strong><br />
  Real requests. Deliberate failures. Yours to run.
</p>

<p align="center">
  <a href="https://httptoy.com">Live playground</a> ·
  <a href="docs/REFERENCE.md#api-endpoints">API reference</a> ·
  <a href="LICENSE">MIT license</a>
</p>

## ⚡ Run locally

Go **1.26+**. One binary, frontend included. No Node server needed.

```sh
git clone https://github.com/TrueFix/httptoy.git
cd httptoy
go run .
```

Open **http://localhost:8000**.

<details>
<summary>🐳 Prefer Docker?</summary>

```sh
docker build -t httptoy .
docker run --rm -p 8000:8000 httptoy
```

</details>

## 🧪 Give your client a bad day

Inspect requests, simulate failures, capture webhooks, test retries, and stream
responses. Built-in **OpenAPI**, **Postman collections**, and **Prometheus metrics**.

```sh
# See what you sent
curl 'http://localhost:8000/anything?hello=world'

# Fail on purpose
curl -i http://localhost:8000/status/503

# Make your client back off
curl -i http://localhost:8000/retry-after/5

# Keep the conversation going
curl -N 'http://localhost:8000/sse?count=3'
```

[Explore all endpoints →](docs/REFERENCE.md#api-endpoints)

## 🛠️ Make it yours

**Go + Gorilla mux** on the backend. **React + Vite + Framer Motion** up front.

[Frontend development](docs/REFERENCE.md#frontend-development) ·
[Tests](docs/REFERENCE.md#backend-checks) ·
[Deployment & limits](docs/REFERENCE.md#limits-and-deployment)

> Use synthetic data only. For remote deployments, configure TLS, access controls,
> and `PUBLIC_URL`. [Deployment notes →](docs/REFERENCE.md#limits-and-deployment)
