# Capsule KWOK stress environment

This environment runs Capsule on the real kind control-plane node of a KWOK
cluster and creates a default workload of 200 Tenants with three Namespaces
each. KWOK also provides ten simulated worker nodes without running workload
containers.

Requirements: Docker, `kwokctl`, `kind`, `kubectl`, `helm`, `make`, and the Go
toolchain.

```sh
./e2e/stress/setup.sh
./e2e/stress/seed.sh
```

The scripts default to the `kwok-capsule-stress` kubeconfig context. Counts can
be overridden without modifying files:

```sh
TENANTS=500 NAMESPACES_PER_TENANT=5 ./e2e/stress/seed.sh
```

Delete only the generated workload, or delete the complete cluster:

```sh
./e2e/stress/cleanup.sh --workload-only
./e2e/stress/cleanup.sh
```

All generated objects carry `projectcapsule.dev/stress=true` and a
`projectcapsule.dev/stress-run` label. Re-running `seed.sh` with the same prefix
is idempotent.
