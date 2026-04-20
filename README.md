# Note Before Reading
This project is extremely young, and as such is subject to frequent changes- even bottom up redesign. In this state, TaskRun is more of a implementation study of a Kubernetes Operator using GoLang. Until an official V1 is tagged and released, this should NOT be used in any production environments. This repository is public for the purpose of user based feedback and contribution. I am one guy, so feel free to put up recommendations, or even an MR if you feel like contributing!

# TaskRun Operator

A Kubernetes operator that replaces raw Jobs and CronJobs with a declarative, typed alternative. Instead of embedding shell scripts in YAML, you compose named steps from reusable `StepDefinition` types, wire outputs between steps using template expressions, and let the operator handle scheduling, execution, status tracking, and log collection.

## Why

Raw Kubernetes Jobs have several friction points at scale:

- Scripts are embedded in YAML — no linting, no reuse, no composability
- No structured outputs — steps cannot pass data to downstream steps
- Status is opaque — you get `succeeded/failed` counts, not per-step detail
- Auth plumbing is manual every time — secrets, tokens, and certs wired by hand per Job

TaskRun solves these by giving each step a defined schema, typed parameters, and declared outputs.

## Concepts

### TaskRun

A `TaskRun` is the top-level CR. It defines an ordered list of steps, an optional cron schedule, optional auth context, and a failure/retry policy.

- Without `spec.schedule` — generates a one-shot Kubernetes `Job`
- With `spec.schedule` — generates a `CronJob` with the configured concurrency policy

### StepDefinition / ClusterStepDefinition

A `StepDefinition` (namespaced) or `ClusterStepDefinition` (cluster-scoped) describes a step type:

- **schema** — JSON Schema that validates parameters at apply-time (webhook) and reconcile-time
- **runner** — optional container image; if set, the step runs as an init container in a Job pod
- **outputs** — named values the step produces, available to downstream steps via template expressions
- **authAware** — whether the step should receive auth credentials from the TaskRun auth block

Resolution order: namespace-local `StepDefinition` first, then `ClusterStepDefinition`. This lets teams override cluster-wide defaults with namespace-specific versions.

### Step execution modes

| Mode | When | How |
|---|---|---|
| **Runner** | `spec.runner` is set on the StepDefinition | Runs as an init container inside a Job pod. Reads params from `/etc/step/params.json`, writes outputs to `/etc/step/outputs/<name>.json`. Steps share an `emptyDir` volume at `/etc/step`. |
| **API-native** | No `spec.runner` | Executed directly in the controller process. No Job is created. Used for Kubernetes API operations (secrets, configmaps, rollout restarts, waits). |

### Step ordering — the two-phase execution model

> **This is the most important constraint to understand before writing a TaskRun.**

The controller executes steps in two phases, regardless of their declared order:

1. **Phase 1 — All runner steps** are batched into a single Kubernetes Job and run as sequential init containers.
2. **Phase 2 — All API-native steps** execute in-process in the controller, after the Job completes.

This means **interleaving runner and API-native steps is not allowed.** All runner steps must be declared before all API-native steps. The controller enforces this at reconcile time and fails with `InvalidStepOrdering` if the constraint is violated.

#### Valid orderings

```yaml
# ✓ All runners, then all API-natives
steps:
  - name: fetch       # runner
  - name: query       # runner  (custom)
  - name: store       # API-native
  - name: restart     # API-native

# ✓ Runners only
steps:
  - name: fetch       # runner
  - name: notify      # runner  (custom)

# ✓ API-natives only
steps:
  - name: read        # API-native
  - name: update      # API-native
  - name: restart     # API-native
```

#### Invalid orderings — rejected at reconcile time

```yaml
# ✗ Runner after API-native
steps:
  - name: store       # API-native
  - name: notify      # runner ← rejected: "step "notify" (runner) cannot follow
                      #           an API-native step"

# ✗ Interleaved
steps:
  - name: fetch       # runner
  - name: store       # API-native
  - name: notify      # runner ← rejected
```

#### Template chaining across phases

Because Phase 1 completes before Phase 2 begins, runner outputs **are** available to API-native steps via template expressions:

```yaml
steps:
  - name: fetch-data    # runner — produces output "body"
    action: http-request
    outputs: [body]

  - name: store-result  # API-native — can reference runner output ✓
    action: secret-update
    params:
      value: "{{ steps.fetch-data.outputs.body }}"
```

The reverse — an API-native step's output referenced by a runner step — is **not possible**, because the runner Job is created and executes before any API-native step runs.

### Template expressions

Step params can reference outputs from earlier steps:

```
{{ steps.<step-name>.outputs.<output-name> }}
```

Multiple references in a single value are supported. Resolution fails fast if a referenced step or output does not exist.

## Built-in step types

The Helm chart ships 10 `ClusterStepDefinition` CRs out of the box:

| Name | Mode | Description |
|---|---|---|
| `http-request` | runner | HTTP client — GET/POST/PUT/DELETE/PATCH with optional headers and body |
| `secret-update` | API-native | Create or patch a key in a Kubernetes Secret |
| `secret-read` | API-native | Read a key from a Kubernetes Secret and expose it as an output |
| `rollout-restart` | API-native | Trigger a rollout restart on a Deployment, StatefulSet, or DaemonSet |
| `configmap-update` | API-native | Create or patch a key in a ConfigMap |
| `wait` | API-native | Sleep for a given duration (respects context cancellation) |
| `assert` | API-native | CEL expression assertion (stub — full CEL integration pending) |
| `shell-exec` | runner | Execute an arbitrary shell command |
| `db-query` | runner | Run a SQL query against a Postgres database |
| `mongo-command` | runner | Run a command against a MongoDB instance |

## Manifest examples

### One-shot TaskRun — fetch a token and store it as a Secret

```yaml
apiVersion: taskrun.io/v1alpha1
kind: TaskRun
metadata:
  name: fetch-and-store-token
  namespace: my-app
spec:
  auth:
    type: oidc
    tokenEndpoint: https://auth.example.com/token
    credentialsFrom:
      secretRef:
        name: my-oidc-credentials
        clientIdKey: clientId
        clientSecretKey: clientSecret
  steps:
    - name: fetch-token
      action: http-request
      params:
        url: https://api.example.com/v1/token
        method: POST
        body: '{"grant_type":"client_credentials"}'
      outputs:
        - body

    - name: store-token
      action: secret-update
      params:
        secretName: app-token
        key: token
        value: "{{ steps.fetch-token.outputs.body }}"
```

### Scheduled TaskRun — nightly secret rotation with rollout restart

```yaml
apiVersion: taskrun.io/v1alpha1
kind: TaskRun
metadata:
  name: nightly-secret-rotation
  namespace: my-app
spec:
  schedule: "0 2 * * *"
  concurrencyPolicy: Forbid
  onFailure:
    retries: 2
    backoff: exponential
  steps:
    - name: rotate-db-password
      action: http-request
      params:
        url: https://vault.example.com/v1/database/rotate-root/my-db
        method: POST
      outputs:
        - body

    - name: update-secret
      action: secret-update
      params:
        secretName: my-db-credentials
        key: password
        value: "{{ steps.rotate-db-password.outputs.body }}"

    - name: restart-app
      action: rollout-restart
      params:
        kind: Deployment
        name: my-app
```

### Custom StepDefinition — namespace-scoped

Teams can define their own step types without touching cluster-wide resources:

```yaml
apiVersion: taskrun.io/v1alpha1
kind: StepDefinition
metadata:
  name: notify-slack
  namespace: my-team
spec:
  schema:
    type: object
    required: [channel, message]
    properties:
      channel:
        type: string
      message:
        type: string
  runner:
    image: ghcr.io/my-org/runners/slack-notify:1.0.0
  outputs:
    - name: messageId
      description: Slack message timestamp
```

Used in a TaskRun in the same namespace:

```yaml
apiVersion: taskrun.io/v1alpha1
kind: TaskRun
metadata:
  name: deploy-and-notify
  namespace: my-team
spec:
  steps:
    - name: deploy
      action: rollout-restart
      params:
        kind: Deployment
        name: my-service

    - name: notify
      action: notify-slack
      params:
        channel: "#deployments"
        message: "Deployment triggered at {{ steps.deploy.outputs.restarted }}"
```

### Reading a secret and using its value downstream

```yaml
apiVersion: taskrun.io/v1alpha1
kind: TaskRun
metadata:
  name: config-sync
  namespace: ops
spec:
  steps:
    - name: read-api-key
      action: secret-read
      params:
        secretName: external-api-credentials
        key: apiKey
      outputs:
        - value

    - name: sync-configmap
      action: configmap-update
      params:
        configMapName: app-config
        key: externalApiKey
        value: "{{ steps.read-api-key.outputs.value }}"

    - name: restart-after-sync
      action: rollout-restart
      params:
        kind: Deployment
        name: app-server
```

## Status

`kubectl get taskruns` shows phase, schedule, last run time, and age:

```
NAME                    PHASE       SCHEDULE      LAST RUN               AGE
fetch-and-store-token   Succeeded                 2026-04-19T10:00:00Z   5m
nightly-secret-rotation Running     0 2 * * *     2026-04-19T02:00:00Z   12h
config-sync             Failed                    2026-04-18T14:23:00Z   1d
```

Detailed per-step status with outputs and logs:

```yaml
status:
  phase: Succeeded
  startTime: "2026-04-19T10:00:00Z"
  completionTime: "2026-04-19T10:00:04Z"
  steps:
    - name: fetch-token
      phase: Succeeded
      duration: 1.234s
      outputs:
        body: '{"token":"eyJ..."}'
    - name: store-token
      phase: Succeeded
      duration: 42ms
      outputs:
        updated: created
  conditions:
    - type: Complete
      status: "True"
      reason: AllStepsSucceeded
```

## Architecture

```
                         ┌─────────────────────────────────────────┐
  kubectl apply ──────►  │              TaskRun CR                  │
                         └───────────────────┬─────────────────────┘
                                             │ watch
                         ┌───────────────────▼─────────────────────┐
                         │           TaskRun Controller             │
                         │                                          │
                         │  1. Resolve StepDefinitions              │
                         │  2. Validate params (JSON Schema)        │
                         │  3. Validate step ordering               │
                         │  4. Partition: runner vs API-native      │
                         │                                          │
                         │  ┌── PHASE 1 ────────────────────────┐  │
                         │  │  Runner steps → Job / CronJob     │  │
                         │  │  (all runners batch into one Job) │  │
                         │  └───────────────────────────────────┘  │
                         │                  ▼ Job completes         │
                         │  ┌── PHASE 2 ────────────────────────┐  │
                         │  │  API-native steps → in-process    │  │
                         │  │  (sequential, with template res.) │  │
                         │  └───────────────────────────────────┘  │
                         │                                          │
                         │  5. Collect logs + outputs               │
                         │  6. Write status + conditions + metrics  │
                         └────────┬──────────────────┬─────────────┘
                                  │                  │
               ┌──────────────────▼───┐   ┌──────────▼──────────────┐
               │  PHASE 1             │   │  PHASE 2                │
               │  Kubernetes Job /    │   │  API-native executors   │
               │  CronJob             │   │                         │
               │                      │   │  secret-update          │
               │  ┌────────────────┐  │   │  secret-read            │
               │  │  auth init     │  │   │  rollout-restart        │
               │  │  step-0-fetch  │  │   │  configmap-update       │
               │  │  step-1-notify │  │   │  wait                   │
               │  │  collect-out   │  │   └─────────────────────────┘
               │  └────────────────┘  │   (runs after Job completes)
               │  /etc/step/ emptyDir │
               │  /etc/step/params/   │
               └──────────────────────┘
```

### Reconciliation loop

1. **Fetch** the `TaskRun`; skip if already terminal (Succeeded/Failed)
2. **Resolve** each step's `action` → `StepDefinitionSpec` (namespace-local first, then cluster-scoped)
3. **Validate** each step's `params` against the resolved JSON Schema
4. **Validate step ordering** — all runner steps must precede all API-native steps; fail with `InvalidStepOrdering` if interleaved
5. **Partition** steps into runner-based and API-native
6. **Phase 1 — Runner steps** — write params ConfigMap; create a `Job` (or `CronJob` if scheduled); requeue until the Job completes; collect per-step logs and outputs from the collector container
7. **Phase 2 — API-native steps** — execute sequentially in-process after Phase 1 completes; template expressions in params are resolved against runner step outputs before each step runs
8. **Status** — write per-step phase, duration, outputs, and logs; set `TaskRun.status.phase` and conditions; record Prometheus metrics

## Installation

```bash
helm install taskrun charts/taskrun \
  --namespace taskrun-system \
  --create-namespace
```

This installs:
- The operator `Deployment`
- CRDs for `TaskRun`, `StepDefinition`, `ClusterStepDefinition`
- `ClusterRole` and `ClusterRoleBinding`
- The 10 built-in `ClusterStepDefinition` CRs

## API reference

### TaskRunSpec

| Field | Type | Description |
|---|---|---|
| `schedule` | string | Cron expression. Omit for one-shot Jobs. |
| `concurrencyPolicy` | `Allow` \| `Forbid` \| `Replace` | How to handle concurrent scheduled executions. Default: `Forbid`. |
| `auth` | `AuthSpec` | Auth context inherited by all `authAware` steps. |
| `steps` | `[]StepSpec` | Ordered list of steps. At least one required. |
| `onFailure` | `FailurePolicy` | Retry and backoff policy. |

### StepSpec

| Field | Type | Description |
|---|---|---|
| `name` | string | Unique identifier within the TaskRun. Lowercase alphanumeric and hyphens. |
| `action` | string | Name of the `StepDefinition` or `ClusterStepDefinition` to use. |
| `params` | `map[string]string` | Input parameters. May contain `{{ steps.<name>.outputs.<key> }}` expressions. |
| `outputs` | `[]string` | Output names to capture for use by downstream steps. |

### AuthSpec

| Field | Type | Description |
|---|---|---|
| `type` | `oidc` \| `basic` \| `mtls` \| `none` | Authentication mechanism. |
| `tokenEndpoint` | string | OIDC token endpoint URL. |
| `credentialsFrom.secretRef` | `SecretKeySelector` | Secret containing credentials. |

### StepDefinitionSpec

| Field | Type | Description |
|---|---|---|
| `schema` | JSON Schema | Validates step params at admission and reconcile time. |
| `runner` | `RunnerSpec` | Container image. Omit for API-native steps. |
| `outputs` | `[]OutputSpec` | Named outputs this step produces. |
| `authAware` | bool | Whether to inject auth credentials from the TaskRun auth block. |

## Development

```bash
# Generate CRD manifests and deepcopy functions
make generate manifests

# Run tests (requires envtest binaries)
make setup-envtest
go test ./...

# Build the operator binary
make build

# Build and push the container image
make docker-build docker-push IMG=ghcr.io/davidkenelm/taskrun:dev
```

**Module:** `github.com/davidkenelm/taskrun`  
**API group:** `taskrun.io/v1alpha1`  
**Go version:** 1.23.6  
**Framework:** Kubebuilder v4 / controller-runtime
