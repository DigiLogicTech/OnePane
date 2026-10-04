# Project sandbox runner

The first executable Project Runtime backend uses a trusted rootless Podman or rootless Docker CLI adapter. It does not pass shell command strings and it does not expose the container engine socket to project workloads.

## Container boundary

A created application container is constrained with:

- rootless container engine
- read-only root filesystem
- all Linux capabilities dropped
- `no-new-privileges`
- `network=none` until the proxy-only network backend exists
- bounded CPU, memory, and PID counts
- restricted `/tmp` tmpfs
- one harness-derived writable bind at `/workspace`
- no user-selected host paths
- no device passthrough or privileged/host namespaces
- `--pull=never` during ensure; image acquisition is a separate reviewed mutation

`project.app.inspect` does not trust the preceding create/start call. It reads the resulting container configuration and calculates `isolation_verified`. A Project application cannot become observed `running` unless a V2 verification confirms both `status=running` and `isolation_verified=true`.

## Tools

Mutation tools (`project.runtime.ensure`, `project.runtime.stop`, `project.app.pull`, `project.app.ensure`, `project.app.stop`) require M15 OperationCoordinator execution permits.

Observation tools (`project.runtime.inspect`, `project.app.inspect`, `project.image.inspect`) use a separate observe capability and never reconcile state.

`project.app.exec` is `EXECUTE_SANDBOXED`. ToolGateway executes that action class only for an adapter identity explicitly allow-listed by trusted bootstrap code. Before executing argv inside the container, the adapter re-inspects the container and requires a verified sandbox boundary.

Secrets and egress remain fail-closed until their dedicated subsystems exist. A `secret_ref` cannot be resolved by arbitrary sandbox code, and applications requesting network egress are rejected until the proxy-only network layer is implemented.

## Project Routine bindings

An active `ProjectRoutineBinding` can target an `app_command`. The binding is only configuration; it grants no authority. The execution path requires a normal Task, worker principal, and scoped CapabilityLease, then invokes `project.app.exec`. Task verification and completion continue through the normal Assurance path.
