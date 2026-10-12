# Development

## Development environment

Install [Devbox](https://www.jetify.com/docs/devbox/installing-devbox/) and its Nix
prerequisite, then use the committed environment:

```sh
devbox shell
task --list
task hooks:install
task check
```

Alternatively, run a command without opening an interactive shell:

```sh
devbox run -- task test:unit
devbox run -- task helm:lint
```

CI uses Devbox 0.18.4 and the same `devbox.json`/`devbox.lock`. These files manage
Go, Task, generators, linters, Kubernetes CLIs, Helm, ko, and prek. Task runs those
tools from PATH; `bin/` contains build output and downloaded envtest API-server
assets, not locally compiled development tools. Go uses the pinned toolchain
without silently downloading another version. Go's race detector also needs the
platform C toolchain (Xcode Command Line Tools on macOS; provided by Nix on Linux).
CI caches Go modules/builds separately from Devbox's Nix packages.

Image builds use ko and need no container daemon. Docker or a compatible container
engine must be installed and running separately for Kind e2e and the Artifact Hub
metadata linter (`task helm:artifacthub`). Devbox does not
install or restart a container daemon.
On macOS, installing Nix requires system-level setup; follow its installer rather
than treating the Taskfile as a system provisioning script.

To upgrade tools, edit their versions in `devbox.json`, run `devbox update`, and
commit both configuration and lockfile. `git` and `bash` use major-version
constraints; their exact builds are recorded in the lockfile. The Nix base commit
is also pinned. Review version changes and rerun `task check`.

## Layout

- `api/v1alpha1/`: cluster-scoped API types and generated DeepCopy methods.
- `cmd/main.go`: Kubebuilder manager entry point and scheme registration.
- `internal/`: controller, admission, scheduling, execution and metrics code.
- `examples/`: executable ChangeWindow and executor RBAC examples.
- `charts/`: Helm packaging, including generated CRDs and RBAC.
- `e2e/`: tests against the built controller in an isolated Kind cluster.
- `hack/`: development tooling tests, native check configuration, and API-reference tooling.
- `Taskfile.yml`: inline developer and CI tasks; `devbox.json`/`devbox.lock`: tools.
- `prek.toml`: native file checks and local development/commit-message hooks.

`PROJECT` retains Kubebuilder metadata and the standard `cmd/main.go` entry point.
Kubebuilder may scaffold build tooling during future commands; keep the Taskfile
as the maintained entry point when incorporating that scaffold.

Helm is the only supported installation method; see the
[installation guide](docs/content/docs/getting-started/installation.md).
CRDs and RBAC are generated directly into the chart; examples live in `examples/`.

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

## Generation and checks

After editing API types or RBAC markers, run `task generate manifests` and commit
the resulting artifacts. `task verify-generated` detects drift, including untracked
generated files. It assumes intended source/generated files have been added to
Git. Generation task calls are serialized and deduplicated within a Task run.

`task fmt` formats Go (including tests) and maintained YAML. `task lint`,
`task lint:yaml`, `task lint:actions`, `task lint:shell`, and `task helm:lint` match
CI checks. YAML validation uses prek's Rust-native parser; yamlfmt checks style.
Generated CRDs/RBAC retain their generator's formatting, and Helm templates are
validated through Helm instead of being parsed as ordinary YAML.
Actionlint uses ShellCheck for workflow commands; its optional Python checker is
disabled. Task implementations live directly in `Taskfile.yml`, including image
builds, releases, documentation, development deployment, and e2e cleanup.
`task lint:shell` checks those inline commands with ShellCheck. Tasks that need
Bash signal handling or timers use an inline Bash block.

`task hooks:install` replaces pre-commit's existing `pre-commit` and `commit-msg`
Git hook shims with prek, without chaining the old Python hooks.
Every hook calls a Task target through `devbox run -- task`, including file
hygiene, YAML formatting, and Conventional Commit validation. The same targets
can be run directly in a Devbox shell. `task lint:files` invokes prek's native
checks through `hack/prek-builtins.toml`; that configuration contains no Task
hooks, avoiding recursion. File hygiene checks report changes without modifying
files; fix reported issues and stage them before retrying. No Python environments
or pip dependencies are used. `task hooks:check` runs all pre-commit hooks on tracked files; new files must
be staged to participate. The generator hook runs before checks that consume its
output.

`task test:tooling` exercises Task variables, failure propagation, native syntax
and commit checks, Helm rendering, and enterprise dry runs without publishing
or accessing a cluster. The legacy hyphenated command names remain Task aliases
where useful, for example `task test-unit` and `task enterprise-release`.

## Runtime and validation

The manager runs as non-root with a read-only root filesystem. cert-manager issues
and rotates webhook certificates. Metrics use HTTPS with authentication and
authorization. Run locally with `POD_NAMESPACE` or `--system-namespace` set and
provide webhook TLS certificates when the manager is reachable by the API server.

`task test:unit` excludes the real API-server `TestAPI`. `task test` runs unit and
envtest integration tests with the race detector. `setup-envtest` is an independent
asset downloader; its pinned version downloads the Kubernetes version specified
by `ENVTEST_K8S_VERSION`. Supply `KUBEBUILDER_ASSETS` to use an existing asset
directory. `GO_TEST_FLAGS` accepts whitespace-separated Go flags, defaulting to
`-race`; shell quoting inside the value is not evaluated.

`task e2e` creates its own Kind cluster, installs pinned cert-manager, and runs
`dev:deploy` twice to verify Helm installation and upgrade of the current source.
It runs the integration tests and collects logs before cleanup. Its private kubeconfig
preserves existing clusters and the developer's current context. Set `IMG`,
`KIND_CLUSTER`, `KIND_NODE_IMAGE`, and `ARTIFACTS` using Task variables or environment
variables. Select a unique cluster name; the harness rejects an existing name.

`task manifests` writes CRDs and manager RBAC directly into the Helm chart. Do not
hand-edit generated manifests. The chart's optional ServiceMonitor requires a Prometheus
token Secret with `/metrics` RBAC.

Run performance checks inside Devbox with:

```sh
go test ./internal/schedule ./internal/admission -run '^$' -bench . -benchmem
```

See `AGENT.md` for lifecycle, isolation, caching and review requirements, and the
README for the API contract and current bounds. Admission-only rules require no
executor permissions; resource actions always use a configured service account.

## Documentation

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

### Hugo version selection

If `hugo version` reports an older Homebrew/system binary, use
`devbox run -- task docs:serve`. Devbox requires Nix. Alternatively, install
Hugo Extended 0.166.0 or newer and Node 24 and put them first on `PATH`.
Documentation tasks reject unsupported Hugo versions before generating or
building files. Use the Task commands to expose the local Sass compiler on
`PATH`; installing the theme does not replace your system Hugo.

The website's [development guide](docs/content/docs/development/contributing.md)
explains architecture, testing, and the contribution/release process.

## Headlamp plugin

The plugin in `headlamp/` uses the pinned Headlamp SDK and Node 24 toolchain.
Run `task headlamp:install` once, then `task headlamp:check` for linting, formatting,
type checks, unit tests, and a production build. `task headlamp:serve` starts the
SDK development watcher; `task headlamp:package` produces a local archive.

For frontend integration tests, point `HEADLAMP_URL` at an existing Headlamp
frontend and run `task headlamp:test:browser`. The suite intercepts Kubernetes
requests with fixtures and does not need cluster credentials. See
[headlamp/README.md](headlamp/README.md) for browser setup, permissions, supported
selection semantics, and installation. These checks do not replace controller
e2e tests when changing runtime behavior.

## Images and releases

`task image:build` uses ko to write `dist/controller.tar`, tagged with `IMG`,
without pushing or loading a container daemon. It builds for the host's Linux
architecture; set `IMAGE_PLATFORM=linux/amd64` to override it. `IMAGE_ARCHIVE`
changes the output path. Kind tests load that archive directly. `task image:ko`
is an alias of `image:build`.

`task image:push` builds and publishes Linux amd64/arm64 images and ko's SBOMs.
Set `PLATFORMS` to override the published platforms. This is an explicit publishing
operation, not a push of the local archive. Both tasks use `.ko.yaml` and write the
result to `IMAGE_REFS` (default `dist/image-refs.txt`). `IMG` accepts a tagged
reference, including a registry port. `image:sign` signs the recorded digest;
the release workflows export its repository and digest for provenance.

GitHub workflows call the same Task targets through the shared Devbox setup action.
Linting, unit/API tests, e2e, documentation, and Headlamp each have their own jobs.
The e2e matrix uses the existing Kind harness; an OpenShift harness is not yet
implemented. External security scanners, artifact uploads, signing setup, chart
index publication, and SLSA generation remain GitHub Actions integrations.

`task release:snapshot` checks GoReleaser and builds archives/SBOMs without
publication or signing. `task release` publishes a tagged GitHub release.
`helm:package` and `helm:push` use the `change-windows` chart and `VERSION`;
`helm:docs` generates its README. Helm schema generation is not configured in this
repository. Release tasks require the signing/SBOM tools installed by their CI jobs.

Enterprise tasks package and publish the matching controller image and Helm chart:

```sh
task enterprise:prerelease ENTERPRISE_VERSION=0.1.0-rc.1 DRY_RUN=true
task enterprise:release ENTERPRISE_VERSION=0.1.0 DRY_RUN=true
```

Inspect dry runs before publication. Release tasks print deployment instructions
after packaging. Pull-request CI never publishes releases.
