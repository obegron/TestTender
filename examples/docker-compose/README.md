# Example: docker compose

This folder contains an example docker-compose wordpress setup. Note that this is not a typical use-case for testtender, however, it does demonstrate some of the nuances you might encounter using testtender. To run this locally, make sure testtender is running with port-forwarding enabled (`testtender server --port-forward`).

```bash
docker compose up -d
docker compose ps
curl -v localhost:8000
docker compose rm -f
```

Building images is not supported, as testtender is not able to do this.