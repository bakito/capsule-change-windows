---
title: Development
weight: 90
---

## Set up a checkout

```shell
git clone https://github.com/projectcapsule/schedule-action-controller.git
cd schedule-action-controller
devbox shell
task hooks:install
task --list
task check
```

[Devbox](https://www.jetify.com/docs/devbox/installing-devbox/) and Nix provide
the tool versions committed in `devbox.json` and `devbox.lock`. On macOS, install
Xcode Command Line Tools for Go's race detector. ko builds container images without
a daemon. Docker or another compatible engine is a separate prerequisite for Kind
tests.

Task implementations live directly in `Taskfile.yml`. Every prek hook calls a
Task target through `devbox run -- task`, including file checks, YAML formatting,
and commit-message validation. `task lint:shell` checks inline Task commands with
ShellCheck. The native checks use a separate builtin-only prek configuration to
avoid calling the Task hooks recursively. No Python environment is required.
`task hooks:install` replaces legacy pre-commit shims. Use
`devbox run -- task COMMAND` to run any target without an interactive shell.

## Run the controller on your host

Like Capsule's development setup, `task dev:setup` installs the CRDs and admission
configuration with Helm, pointing the Kubernetes API server at your host on HTTPS
port 9443. The controller itself runs locally with `task dev:run`, so rebuilding
an image is unnecessary. Use a development cluster and a host address reachable
from its API server (not localhost).

```sh
devbox shell
export DEV_CONTEXT="$(kubectl config current-context)"
export DEV_HOST=192.168.1.100 # Your host address reachable from the cluster
task dev:setup
task dev:run
```

`LAPTOP_HOST_IP` is accepted as an alias for `DEV_HOST`; `dev-setup` and
`dev-run` are task aliases. For Docker Desktop, `host.docker.internal` can be
used when the API server can resolve it. IPv4, IPv6, and DNS names are supported.
Allow inbound TCP 9443 from the cluster through the host firewall.

The tasks require an explicit `DEV_CONTEXT` and never change your current
kubeconfig context. Both tasks default to namespace `change-windows-system`,
release `change-windows`, and certificates in ignored `bin/dev/serving-certs`.
Override `DEV_NAMESPACE`, `DEV_RELEASE`, or `DEV_CERT_DIR` as needed, keeping
these settings consistent between setup and run. Use a separate certificate
directory for each development cluster.

Setup creates a local TLS certificate with your host in its subject alternative
names and installs its CA bundle into both webhooks. Valid certificates are reused
on repeated setup. No cert-manager, in-cluster controller, or private-key Secret
is needed. Run uses the selected kubeconfig credentials, including their existing
RBAC permissions; it does not create an elevated service account.

Admission remains fail closed: selected API writes fail while the local process
is stopped. To remove the development webhooks, uninstall the release:

```sh
helm --kube-context "$DEV_CONTEXT" uninstall change-windows --namespace change-windows-system
```

Use your overridden release and namespace if different. CRDs and ChangeWindow
objects are retained to avoid deleting development data.


## Deploy your working tree into a cluster

`task dev:deploy` builds the current source with ko and installs or upgrades the
controller with Helm. It requires an existing development cluster, an explicit
`DEV_CONTEXT`, and cert-manager installed in that cluster. It waits for the
Deployment, serving certificate, and CA injection, then submits a dry-run
ChangeWindow through the API server to verify admission TLS.

For an existing Kind cluster, load the image directly without a registry:

```sh
devbox run -- task dev:deploy DEV_CONTEXT=kind-my-dev DEV_KIND_CLUSTER=my-dev
```

Run these commands from the repository root. For repeated commands, enter
`devbox shell` once and use `task dev:deploy` inside it. A bare `task` outside
Devbox does not load the pinned tools: errors such as a missing `controller-gen`
mean the development environment is not active. The task checks its required
tools before generating code, building an image, or accessing the cluster.

Kind is the default `DEV_IMAGE_MODE`. The task verifies that the selected context
points to that Kind cluster, builds for its node architecture, and uses a fresh
image tag on every invocation. Uncommitted source changes therefore reach a new
Pod even when the base `IMG` tag is unchanged. The image pull policy is `Never`.
Kind mode sets `manager.hostUsers=true` for container-runtime compatibility;
the controller still runs as a non-root user.

For another cluster, explicitly select registry publishing and a repository you
can push to. The cluster must be able to pull from the same registry:

```sh
devbox run -- task dev:deploy DEV_CONTEXT=my-dev DEV_IMAGE_MODE=registry \
  IMG=ghcr.io/my-org/change-windows:dev
```

Registry mode reuses `task image:push` and deploys its immutable digest. Authenticate
with `task registry:login` or `ko login` first. `PLATFORMS` controls the published
architectures; its default is `linux/amd64,linux/arm64`.

Both modes use `DEV_NAMESPACE=change-windows-system` and
`DEV_RELEASE=change-windows` by default. `DEV_TIMEOUT` sets the timeout in seconds
for each readiness wait (default 180). Pass `DEV_HELM_VALUES=path/to/dev-values.yaml`
for additional chart settings, such as resource limits or service-account image
pull secrets. The task resets previous Helm values before applying that file and
its development overrides, so supply the file on every deployment. It restores
in-cluster webhook URLs and certificate generation when replacing `dev:setup`.
Stop `dev:run` before switching to an in-cluster controller for the same release.

The task changes only the explicitly selected context and does not create or
remove clusters. Uninstall with Helm when finished; the inline CRD and its
ChangeWindow objects are retained:

```sh
helm --kube-context my-dev uninstall change-windows --namespace change-windows-system
```

## Architecture and repository map

| Location | Responsibility |
| --- | --- |
| `api/v1alpha1/` | API types, validation markers, and generated DeepCopy code. |
| `internal/schedule/` | Bounded cron-window evaluation and cached boundaries. |
| `internal/policy/` | Selector/template compilation and immutable indexed policy store. |
| `internal/executor/` | Impersonated reads, captures, and identity-constrained SSA. |
| `internal/controller/` | Durable journals, finalizers, and start/end reconciliation. |
| `internal/admission/` | Policy validation, matching, denial, warnings, and cache wiring. |
| `internal/metrics/` | Bounded-label Prometheus metrics. |
| `cmd/main.go` | Manager, discovery, dependencies, TLS, and registration. |
| `charts/` | Helm chart and generated CRDs/RBAC. |
| `examples/` | Executable ChangeWindow and executor RBAC examples. |
| `e2e/`, `hack/` | Isolated Kind tests and development/release helpers. |
| `docs/` | Hugo site, pinned Doks npm dependencies, guides, and generated API reference. |
| `headlamp/` | Headlamp plugin, schedule previews, selection helpers, and UI/browser tests. |

Reconciliation persists captures before mutation and resumes from its Secret
journal after retries or leader changes. Admission consumes compiled indexed
policies and evaluates time independently of reconciled status. Changes to either
path must preserve namespace scope, protected namespaces, and object identity.

Helm is the only supported [installation method]({{< relref "/docs/getting-started/installation" >}}).
CRDs and RBAC are generated directly into the chart.
Keep user-facing deployment instructions aligned with the Helm chart.

## Change and generate the API

```shell
task fmt generate manifests docs:generate
task verify-generated docs:check
```

Edit Go API types and validation markers first. Generate DeepCopy methods, CRDs,
RBAC, and the API reference; never hand-edit those outputs. `task manifests`
writes directly into the chart's CRD and RBAC directories. `task verify-generated` expects intended
files to be tracked by Git and detects modified or untracked generated artifacts.

Task dependencies run concurrently. Keep generation before consumers using
ordered task commands, following the existing Taskfile. Kubebuilder may scaffold
build files during API changes; retain Task as the maintained workflow.

## Tests and validation

```shell
task test:unit
task test
task test:tooling
task lint lint:yaml lint:actions lint:shell helm:lint
task e2e IMG=ghcr.io/projectcapsule/change-windows:e2e \
  KIND_CLUSTER=scheduled-action-dev
```

`test:unit` excludes the real API-server suite. `test` runs unit and envtest
integration tests with the race detector; setup-envtest obtains the pinned API
server assets. Set `KUBEBUILDER_ASSETS` to an existing directory to reuse them.
`test:tooling` verifies Task, hook, Helm, and release-command behavior without
publishing or touching a cluster. `docs:check` checks generated-reference drift,
example validity, and rendered links and assets.

The e2e task creates a new isolated Kind cluster with a private kubeconfig,
installs cert-manager, and runs `dev:deploy` twice to check Helm installation and
upgrade of the current source. It runs the integration tests and collects artifacts before
cleanup. Choose a unique cluster name. It rejects an existing cluster and does
not reuse the developer's current context.

New scheduling, admission, or RBAC behavior needs allowed/denied cases, selected
and unselected namespaces, and scoped e2e coverage. Cover retries, replacement
UIDs, label changes, finalizers, dry runs, subresources, ownership conflicts, and
capture retention where applicable. Use real API-server tests for schema and SSA
behavior; fake clients cannot establish these guarantees.

```shell
go test ./internal/schedule ./internal/admission \
  -run '^$' -bench . -benchmem
```

Benchmark changed scheduling and admission paths, with allocation reporting.
Reuse immutable compiled state, avoid per-request compilation or cluster-wide
scans, keep metric labels finite, and never log captured values. Pass cancellation
contexts through API calls. Report unrun checks and environment failures clearly.

## Work on the documentation

The site uses [Doks](https://getdoks.org/) with **Hugo Extended 0.166.0 or
newer** and **Node 24**, pinned in Devbox. From the repository root:

```shell
devbox shell
hugo version
task docs:serve
# Production build, including a project-site base path:
task docs:build DOCS_BASE_URL=https://example.com/scheduled-actions/
task docs:check
```

Open <http://localhost:1313> for the live preview. Documentation tasks install
locked npm dependencies automatically. Doks and Dart Sass
are pinned in `docs/package.json` and `docs/package-lock.json`; no Python tools
are needed. Installation uses `npm ci --ignore-scripts`. To update a dependency,
use `npm --prefix docs install --save-exact PACKAGE@VERSION --ignore-scripts`,
then run the documentation checks and inspect the rendered site.

Edit weighted Markdown pages under `docs/content/docs/`. Group indexes define
the sidebar. Use Hugo `relref` links between pages so navigation also works on
project sites. The shared `example` shortcode and download mounts read directly
from `examples/`; do not duplicate executable examples.

The homepage explorer renders `examples/quickstart.yaml` without changing
its content. Annotations in `docs/data/window_example.yaml` connect each top-level
spec block to its explanation, guide, and API type. Keep those links accurate as
the API evolves. The static checks verify that copyable YAML and the download
exactly match the executable sample.

`docs/content/docs/reference/api.md` is generated by the pinned `crd-ref-docs`
Go tool in `hack/tools/`. Run `task docs:generate` after changing API types.
The generator's dependencies stay outside the controller module. The empty
`docs/go.mod` also prevents Go tools from traversing npm dependencies.

`task docs:check` checks generated-reference drift, validates examples, and
builds root and project-site variants. It verifies every page's local links,
anchors, assets, search index, and downloadable examples. For theme changes,
also check search, code copying, dark mode, and mobile navigation in a browser.
The Sass partial suppresses only known deprecations in Doks/Bootstrap; other
build warnings still fail validation.

For interactive documentation changes, install Chromium once and run the browser
suite. It covers desktop/mobile, keyboard selection, copying, dark mode, and the
no-JavaScript fallback on a project-site URL:

```shell
npm --prefix docs exec -- playwright install chromium
task docs:test:browser
```

CI installs Chromium with its system dependencies and runs the same tests.

Production output is in `dist/docs/`. The Documentation workflow checks the site
and uploads a preview artifact for pull requests. Every push to `main` also
deploys the checked site to GitHub Pages; it can be rerun manually on `main`.
Builds use the base URL returned by Pages, including a configured custom domain.

In the repository settings, select **Pages → Build and deployment → Source →
GitHub Actions** once. The workflow uses the built-in token and the `github-pages`
environment; no publishing secret is required.

### An older system Hugo

If `hugo version` reports an older Homebrew/system binary, use
`devbox run -- task docs:serve`. Devbox requires Nix. Alternatively, install
Hugo Extended 0.166.0 or newer and Node 24 and put them first on `PATH`.
Documentation tasks reject unsupported Hugo versions before generating or
building files. Use the Task commands to expose the local Sass compiler on
`PATH`; installing the theme does not replace your system Hugo.

## Develop the Headlamp plugin

```shell
task headlamp:install
task headlamp:check
task headlamp:serve
```

The plugin has its own npm lockfile and uses the Node version pinned in Devbox.
`headlamp:check` runs lint, formatting, TypeScript, unit/UI tests and the production
build. Browser integration against a reachable Headlamp frontend uses
`HEADLAMP_URL=... task headlamp:test:browser` with intercepted Kubernetes fixtures.
See the [Headlamp guide]({{< relref "/docs/guides/headlamp" >}}) for installation and the resource maintenance
topic. Keep cluster scoping and owner UID checks intact, and distinguish current
selection from the controller's durable action snapshot.

## Contributing and releases

### Container images

```shell
task image:build IMG=registry.example.com/scheduled-action:dev
# Explicitly publish a multi-platform image when ready:
task image:push IMG=registry.example.com/scheduled-action:dev
```

Both targets build with ko. The first creates `dist/controller.tar` for the host's
Linux architecture without pushing to a registry. Set `IMAGE_PLATFORM` or
`IMAGE_ARCHIVE` to change the platform or output path. Kind e2e loads this archive
directly. `image:push` builds and publishes Linux amd64/arm64 images and SBOMs;
override `PLATFORMS` if needed. `image:ko` is an alias of `image:build`.

GitHub workflows invoke the same Task targets with the locked Devbox toolchain.
`release:snapshot` checks and builds GoReleaser archives without publication or
signing; `release` publishes a tagged release. Helm tasks use `charts/change-windows`.

Follow the repository's [contribution guide](https://github.com/projectcapsule/schedule-action-controller/blob/main/CONTRIBUTING.md)
and [agent/development conventions](https://github.com/projectcapsule/schedule-action-controller/blob/main/AGENT.md).
Keep changes focused, use Conventional Commits, include Apache-2.0 Go headers, and
run the checks relevant to the change before opening a pull request.

```shell
task enterprise:prerelease ENTERPRISE_VERSION=0.1.0-rc.1 DRY_RUN=true
task enterprise:release ENTERPRISE_VERSION=0.1.0 DRY_RUN=true
```

Enterprise release tasks publish images and Helm charts when `DRY_RUN` is omitted.
Review the preview and authenticate to the intended registry before intentional
publication. Versions use SemVer without a `v` prefix or build metadata. Release
tasks also print deployment instructions. Pull-request checks do not publish releases.
