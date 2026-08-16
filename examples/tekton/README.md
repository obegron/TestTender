# Example: Tekton

This folder contains an example Tekton Task and Pipeline that use an existing
TestTender Service to run the `testcontainers-java` example. Deploy TestTender
in the same namespace first; the Task connects to `tcp://testtender:2475` and
does not run Docker-in-Docker or a TestTender sidecar.

Apply the resources:

```bash
kustomize build . | kubectl apply -f -
```
Start a pipelinerun via cmd:

```bash
tkn pipeline start testtender-example
        -p git-url=https://github.com/obegron/TestTender.git \
        -p context-dir=examples/testcontainers-java \
        -p git-revision=main
```

Or start a pipelinerun via the provided yaml-file:

```bash
kubectl create -f ./resources/example/pplr_testtender.yaml
```

The example deliberately uses the namespace Service topology. Tenant test
compute stays in the tenant cluster and the Testcontainers client communicates
with TestTender over the cluster network.
