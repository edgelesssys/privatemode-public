# Testing workload liveness against a live deployment

This is a testing protocol for an AI Agent for verifying the workload liveness mechanism
implemented in this package ([`probe.go`](./probe.go)) against a real deployment.
Introduced in [#3726](https://github.com/edgelesssys/continuum/pull/3726).

A full run is roughly 4h of wall clock, most of it unattended — the timed tests alone are over
three, before the vLLM restarts between them.

## Orientation

The workload container's `livenessProbe` targets `/workload-livez` on the inference-proxy, so the
**proxy**, not the kubelet, decides whether the workload is restarted. Read `CheckLiveness` before
starting — it is the entire decision in one function, and the tests below are written against it.

Two things that are easy to misread:

- `probe` in that package means an inference request the proxy issues on its own loop. It is not the
  kubelet's probe, and the two have independent intervals and timeouts.
- The budget switches on reachability, but the clock it is measured against only advances on
  specific events. Which events those are is the crux of tests 7-10, so read that part carefully
  rather than inferring it.

The timeouts live in two places: the proxy-side constants at the top of `probe.go`, and the
kubelet-side probe settings in the workload chart. Both matter for the predictions below; take the
current values from there rather than from this document.

## Ask the user first

None of this can be inferred safely. A wrong guess could break production.

- **Kubeconfig path, and which cluster it is.** Ask for the path, confirm with
  `kubectl config current-context` before the first command, and pass `--kubeconfig` every time.
- **Namespace**, and if there are any constraints within.
- **Who applies the dev-mode chart edit** below — you or the user.
- **Whether load generation is allowed**, and which local proxy port targets the test deployment.
  The obvious port may point at production; see test 12.
- **When the nightly e2e run fires**, if the full run is planned — it competes for GPUs.

## Guardrails

- Stay inside the agreed namespace. The user must approve any Kubernetes operations outside of that namespace.
- Only `exec` into the workload pod under test and the unstructured pod (test 11 reads its marker).
- Never kill a local process you did not start. A `privatemode-proxy` already running locally probably
  belongs to the user and may be connected to production.
- Always kill specific local PIDs found with `pgrep -f` (disable truncation of `comm` field).
- The chart edit below is local scaffolding and must not reach a commit.

## Deployment to request

Ask the user to deploy per [test-deployment.md](../../../dev-docs/test-deployment.md) with:

| Setting | Value | Why |
|---|---|---|
| `DEV_WORKLOADS` | `true` | shell in the workload container, and vLLM can be stopped without the container exiting |
| `INSECURE_POLICY` | `true` | required for `exec` under Contrast |
| `FAKE_WORKLOADS` | `false` | the fake workload writes its own marker and bypasses the chart's wrapper |
| `ENABLE_WORKLOADS` | one real model running in vLLM, e.g. `gpt-oss-120b,1,snp` | real model load time is what test 2 measures |

The unstructured deployment comes with the same chart and needs no extra configuration. Leave it
running: it supplies test 11, the non-dev configuration, for free.

**Dev mode templates the probes out.** Both probes sit behind `{{- if not $.Values.workloads.dev }}`
in `charts/continuum-application/templates/workload/deployment.yaml`. To test the liveness path in a
dev deployment, change the template to close that guard after `readinessProbe` so `livenessProbe` renders
unconditionally. Consequence while it is applied: a dev pod where nobody starts vLLM kills itself every
hour, which is correct behaviour and not a finding. Revert it before committing anything.

The edit must be in place before the deploy.

## Derive the concrete values

Do not hardcode ports or paths — render them with `helm template` mirroring `generate-contrast-manifest.sh`.

Render with `workloads.dev` both `true` and `false` and confirm the probe renders in both. For model
index `i`: metrics port `8185+i`, workload port `8000+i`, marker
`/var/run/workload-marker/<escapedModelName>-<i>`. The unstructured deployment uses fixed port 8185,
marker `/var/run/workload-marker/unstructured-api`, and `/healthcheck` as its reachability URL.

Then confirm the deployed pod spec matches the render before trusting anything else: same probe
settings, marker volume mounted read-write in the workload container and read-only in the proxy,
both marker flags on the proxy. The render is the expectation — never check against values copied
into this document. A mismatch here means the manifest was generated before the chart edit, and
nothing below is meaningful.

## Method

- **Write the prediction before each timed test** — expected time *and* what result would falsify it.
  The timeouts are exact enough that a prediction to the second is meaningful.
- **The proxy's own logs are the primary evidence.** Background polling loops are secondary; they die
  with your terminal, the in-cluster logs do not. Capture them continuously (`kubectl logs -f` to a
  file) for the whole run: the gpt-oss proxy emits enough request-path chatter to rotate its
  container log in well under an hour, so by the last test the first test's startup lines are gone.
- **Sampling `/workload-livez` is not passive — it runs `CheckLiveness`.** Every sample refreshes the
  generation and reachability, so it emits the marker-error lines the fail-open tests count, and can
  itself renew `lastProgressAt` on a reachable transition. Keep the sampling interval coarse
  (~10s) and state it when reporting counts. In test 10 a fine-grained sampler inflates the
  transition count on its own.
- **Sample with `python3`, not `curl`** — `python3` is present in the vLLM image, `curl` is not
  guaranteed, and a missing binary produces no samples rather than an error.
- **Compare timestamps from the proxy log**, which are the container's clock, not your shell's. The
  tolerance everywhere in this document is one `periodSeconds` plus your sampling interval — nothing
  looser. A deviation beyond that band is a real result, so keep the sampling interval tight enough
  that the band stays meaningful: polling every two minutes cannot resolve a timeout to the second.
- **Verify your instruments.** `ss` is absent from the vLLM container after a restart and returns
  empty with exit 0, which is indistinguishable from "port closed" — use a TCP connect from Python.
  A stub that serves HTTP/1.1 keep-alive will not produce reachability transitions, because the
  proxy reuses one pooled connection; send `Connection: close`.
- **Run non-destructive tests first** (fail-open, marker edits), kill-inducing tests last.
- **Restore vLLM after every test that ends in a restart**, otherwise the next test starts from a
  workload that is unreachable rather than serving.
- The proxy container is distroless with no shared PID namespace, so it cannot be `exec`ed into or
  restarted on its own.
- Most tests here are designed to drive the system into a failure state. A run that reports "passed"
  without evidence that the failure state was actually reached is worse than no run — check that the
  stub, the kill, or the load actually took effect before believing the result.

### Working in the workload container

In dev mode the container runs `sleep inf` as PID 1, so vLLM is yours to start and stop without the
container exiting. The wrapper also installs `tmux` and writes `start.sh` into the working directory
(`/vllm-workspace`), holding `vllm serve` with exactly the arguments from the pod spec — use it
rather than reconstructing the argument list.

Three `exec` details that cause silent, expensive failures:

- **`kubectl exec ... -- bash -s <<EOF` needs `-i`.** Without it there is no stdin and the command
  hangs forever instead of erroring. Use `-it` only for interactive shells.
- **`exec` connections drop regularly.** Anything started in the foreground of an `exec` dies with
  it, usually unnoticed — start every long-running thing in `tmux`.
- **Deliver scripts base64-encoded.** Heredocs nested through `kubectl exec ... bash -c '...'` get
  mangled by the intervening quoting; encode the file, decode it in the container, and syntax-check
  it there before running.

Start vLLM, one log per run so a retry cannot match the previous attempt's lines:

```bash
kubectl exec -n "$NS" "$POD" -c "$WORKLOAD_CONTAINER" -- bash -c '
  cd /vllm-workspace && : > vllm.log
  tmux new-session -d -s vllm "bash /vllm-workspace/start.sh > /vllm-workspace/vllm.log 2>&1"'
```

Model load takes minutes. Wait for `"Workload probe succeeded"` in the proxy log rather than polling
the port — that line proves the whole path works, a bound port only proves the socket exists.

Stopping vLLM needs both steps: **killing the tmux session does not stop the engine.** The
`vllm serve` process and its `VLLM::EngineCore` child outlive the session and keep GPU memory
resident, so a stub started immediately afterwards may find the port still held.

```bash
kubectl exec -n "$NS" "$POD" -c "$WORKLOAD_CONTAINER" -- bash -c '
  tmux kill-session -t vllm
  pids=$(ps -eo pid,args --no-headers | grep -E "vllm serve|VLLM::EngineCore" | grep -v grep | awk "{print \$1}")
  kill $pids'
```

Then confirm the port is really closed before starting anything in its place. `ss` is not reliably
present in the container, so check with a TCP connect:

```bash
python3 -c "
import socket
s = socket.socket(); s.settimeout(3)
try:
    s.connect(('127.0.0.1', 8000)); print('OPEN')
except Exception as e:
    print('CLOSED', type(e).__name__)"
```

GPU memory should also drop back (`nvidia-smi --query-gpu=memory.used --format=csv,noheader`).

### Stub servers

Several tests replace vLLM with a stub on the workload port, to control the failure shape while the
workload stays reachable. All three answer `/v1/models` with 200 and differ only in how they handle
`POST /v1/chat/completions`:

| Stub | Inference behaviour | Used by |
|---|---|---|
| refuse | immediate 503 | reachable timeout |
| hang | accepts the body, never responds | reachable timeout, wedged probes |
| flap | immediate 503, listener torn down and rebuilt every ~145s | reachability renewal |

Write them with `python3` (present in the vLLM image), deliver them base64-encoded to avoid quoting
damage, and run each in its own tmux session. The flap stub must set `Connection: close`.

To install a stub: stop vLLM as above, confirm the workload port is refused, then start the stub in
its own tmux session.

## Tests

Run order matters. Tests 1 and 2 start from a freshly started pod with vLLM not yet running; test 2
then starts it and test 3 reads the result. Tests 5-6 are non-destructive and run against a serving
workload. Tests 7-10 each end with the workload container being killed, so run them last and restore
vLLM in between. Test 11 is passive and 13 is arithmetic over logs you already have.

| # | Test | Duration | Ends in a restart |
|---|---|---|---|
| 1 | startup wiring | ~5m | no |
| 2-3 | startup suppression, reachable transition | model load time | no |
| 5-6 | fail-open: deleted, empty | ~5m | no |
| 7 | reachable timeout | ~17m | yes |
| 8 | reachable timeout, wedged probes | ~17m | yes |
| 9 | unreachable timeout | ~62m | yes |
| 10 | reachability renewal | ~35m | no |
| 4 | restart detection | — | verified from 7 or 8 |
| 11 | unstructured | passive | no |
| 12 | overload | ~45m | no |
| 13 | proxy restart budget | — | no |

### 1. Startup wiring

Run before starting vLLM. The wrapper writes the marker as its first action, ahead of the package
installs and `exec sleep inf`, so the marker and the renewal both land within seconds of the
container starting. `"Workload started"` means the workload *container* started — not that the model
server is serving.

**Do:** read the proxy container log from the beginning of the pod's life, and `cat` the marker
inside the workload container.

**Pass:** the proxy logs `"Starting inference proxy"` followed within about a second by
`"Starting HTTP server without TLS"` whose `endpoint` field is the metrics port — that message has
several call sites, so match on the endpoint, not the message — with `restartCount` 0; the marker holds a
UUID; and `"Workload started, renewing its timeouts"` carries a `workloadGeneration` equal to the
marker contents with whitespace stripped. That last equality also proves the proxy's read-only mount
works, so it needs no separate check.

**Fails if:** the proxy exits with `checking workload start marker directory` — the marker volume is
not mounted where `--workload-start-marker` points, and nothing else in this protocol will work.

### 2. Startup suppression

The case the feature exists for. Run it with vLLM **not** started.

**Do:** from inside the workload container, sample `http://127.0.0.1:<metricsPort>/workload-livez`
every few seconds for comfortably longer than the kubelet's `failureThreshold` window, while watching
`restartCount`. Then start vLLM in tmux and keep sampling across the entire model load.

**Pass:** livez returns 200 at every sample, before and throughout the load, and `restartCount` never
increments. `/workload-readyz` returning 500 while vLLM is down is correct and expected — readiness
is a separate path.

**Fails if:** any 503, or any restart. That is precisely the regression this change prevents.

**Start from a fresh container.** `timeoutUnreachable` is measured from the last progress event,
which for a pod nobody has touched is the container start. Starting vLLM on a pod that has been
sitting idle longer than that budget means it has already expired, and the workload will be killed
mid-load through no fault of the change.

### 3. Reachable transition

**Do:** once vLLM is serving (end of test 2), grep the proxy log.

**Pass:** both `"Workload became reachable"` (carrying the reachability URL) and
`"Workload probe succeeded"` appear after vLLM bound its port. **Order is not a criterion** — the two
come from independent goroutines, the reachability line from a kubelet-driven `CheckLiveness` and the
probe line from the proxy's own loop, so either can land first. The transition is what switches the
budget, so its absence means every later timing prediction is anchored wrongly.

### 4. Restart detection

Verified from test 7 or 8; it needs no separate setup. **Not** from test 9 — there the workload was
already unreachable before the kill, so no reachability transition is logged on restart and the
ordering criterion below has nothing to compare against.

**Do:** after a kill, read the new marker and the proxy log around the restart.

**Pass:** the marker UUID differs from the previous one; `"Workload started, renewing its timeouts"`
carries the new generation; that line is timestamped **before** the `"Workload became unreachable"`
line from the same check; and livez returns to 200 rather than the workload being killed again.

**Fails if:** the renewal lands after the reachability downgrade *within the same check* — the
restarted container would inherit the reachable budget instead of the unreachable one, and could be
killed mid-load. Ordering within one `CheckLiveness` is deterministic, so this is a real signal. What
is **not** a failure: a downgrade logged on one check and the renewal on the next, which happens when
a kubelet probe falls between the container starting and the wrapper writing the marker. Compare
sub-second timestamps and confirm both lines came from the same check before calling it.

### 5. Fail-open: marker deleted

**Do:** save the current UUID, delete the marker, sample livez for ~20s, then restore the **same**
UUID and sample again. Restore the way the wrapper writes it — to a `.tmp` file then `mv` — so the
proxy cannot read a half-written file and log a spurious `"marker is empty"`.

**Stop any background sampler first.** The error lines carry no request ID, so a line driven by the
kubelet is indistinguishable from one your own sampling caused, and the count below is then
uninterpretable. Either pause the sampler for the counting window and take a known number of manual
samples, or count only lines falling on the kubelet's `periodSeconds` cadence.

**Pass:** `"Failed refreshing generation marker for reachable workload"` at ERROR once per *kubelet*
probe — `refreshGeneration` runs from `CheckLiveness`, so roughly one line per `periodSeconds`, not
one per inference probe; livez 200 throughout; and **zero** renewal lines after restoring.

**Fails if:** livez goes 503 (the mechanism must fail open), or a renewal appears on restore — that
would mean the in-memory generation is cleared on error, so any transient read failure would
masquerade as a container restart.

### 6. Fail-open: marker empty

**Do:** truncate the marker to zero bytes, wait ~15s and sample livez; then write whitespace only,
wait ~15s and sample again; restore the original UUID.

**Pass:** both phases log `"marker is empty"` — distinct from the `"reading marker:"` error of test 5
— and livez stays 200 in both.

### 7. Reachable timeout

**Do:** stop vLLM, install the **refuse** stub, and take the `"Workload became reachable"` timestamp
that follows as the anchor. Write the prediction down before polling: 503 at anchor +
`timeoutReachable`, SIGKILL one `failureThreshold` window later. Poll `restartCount` and livez.

**Pass:** first 503 at anchor + `timeoutReachable`, within one `periodSeconds` plus your sampling
interval; container terminated with exit code 137 after `failureThreshold` consecutive failures —
that is `(failureThreshold - 1) × periodSeconds` from the first 503, so expect a few seconds under
the round number; the 503 body names elapsed time, the limit and the generation, and appends the last
probe error **only when one has been recorded** (`lastErr != nil`), so do not require that clause
unconditionally; kubelet events show `failureThreshold` `Unhealthy` events per `Killing`.

**Fails if:** the 503 is materially early or late, or no kill follows it.

### 8. Reachable timeout with wedged probes

**Do:** as test 7 but with the **hang** stub. Confirm from the stub's own log that a POST actually
arrived and is being held before trusting anything. While a probe is wedged, sample livez latency.

**Pass:** same schedule as test 7 — a hanging probe must buy the workload nothing, because only a
*successful* probe advances the clock. livez latency stays far below the kubelet's `timeoutSeconds`,
proving `CheckLiveness` does not block behind the probe loop. The probe error logged once
`probeTimeout` expires is a context deadline.

**Note:** `probeTimeout` is a large fraction of `timeoutReachable`, so only one or two probe attempts
fit in the window and `last probe error` can be up to `probeTimeout` stale. Expected, not a fault.

### 9. Unreachable timeout

**Do:** stop vLLM and leave port 8000 dead with the container alive. The anchor is the **last
successful probe**, not the moment it became unreachable — going unreachable does not renew the
clock. Write the prediction and its falsifiers down, then poll every couple of minutes.

**Pass:** no 503 before anchor + `timeoutUnreachable`; SIGKILL one `failureThreshold` window later.

**Fails if:** a 503 appears near anchor + `timeoutReachable` (the reachable budget was wrongly
applied), or nothing has happened shortly after anchor + `timeoutUnreachable` (something is renewing
the clock while unreachable).

### 10. Reachability renewal

**Do:** install the **flap** stub with a full cycle well under `timeoutReachable` and a down phase
of at least several `periodSeconds` — reachability is only sampled once per kubelet probe, so a short
down phase is never observed and the test silently does nothing. Run for at least twice
`timeoutReachable` past the last successful probe, so that a workload without the renewal would have
been killed twice over.

**Pass:** the proxy log shows a `became unreachable` / `became reachable` pair per cycle — expect
roughly (run length ÷ cycle length) of them, and at least enough that the longest gap between
consecutive `became reachable` lines is below `timeoutReachable`; **zero** `"probe succeeded"`;
`restartCount` unchanged **from the start of this test** (earlier tests will have incremented it);
livez 200 throughout. Each transition to reachable renews the clock, so the workload survives
indefinitely without ever serving a successful inference.

**Check before believing it:** if only one `"became reachable"` appears, the stub is holding a
keep-alive connection open and the test never ran. Fix the stub, not the reading.

### 11. Unstructured deployment

Passive — it runs the same mechanism in the **non-dev** configuration, with both probes active, a
multipart probe kind (`/general/v0/general`) and `/healthcheck` as its reachability URL.

**Do:** leave it alone for the duration of the run, then read its proxy log over the whole window and
`cat` its marker.

**Pass:** 0 restarts; probe successes accruing at roughly one per `probeInterval`; 0 probe failures;
marker matching the logged generation; and **exactly one** `"Workload became reachable"`, logged at
proxy start. `New` initialises the workload as unreachable, so the first successful check always
records a transition — a second one means it genuinely flapped. Its log volume is low enough to
retain a full day, unlike the gpt-oss proxy's.

### 12. Overload

**Do:** verify the target first — the scripts under `test/load/` hardcode `http://localhost:8080` in
`test/load/common/textgen.ts`, and a long-running privatemode-proxy on 8080 may be pointed at
**production**. Start a second proxy with `PORT=8081 just run-proxy` against the test deployment's
workspace, confirm `GET localhost:8081/v1/models` lists only the models of the deployment under test,
and run a copy of the load script retargeted to 8081 from outside the repo. Base it on
`test/load/overload/textgen-gptoss-smallInp.ts` — its continuous ramp to a high VU count is the shape
that saturates the API server; the `largeInp` variant is prefill-bound and stresses something else.
See [load-tests.md](../../../dev-docs/load-tests.md). Monitor reachability transitions, probe results, `restartCount`
and livez latency throughout.

**Pass:** two conditions, both required. The load genuinely saturated the workload — take ≥10% failed
requests and a p95 in the minutes as the bar — **and** the mechanism did not misfire: 0 reachability
transitions, 0 probe failures, 0 restarts, livez latency inside the kubelet's `timeoutSeconds`.

**Void, not a pass:** if the client was the bottleneck and the workload never saturated, the run
proves nothing — raise the VU count or check the local proxy is not the limit.

### 13. Proxy restart budget

If the proxy container restarts, the workload's liveness probe fails until the proxy serves again,
with only the `failureThreshold` window to spare.

**Do:** forcing a proxy-only restart requires adding `shareProcessNamespace: true` to the pod spec,
which needs a redeploy. Without it, bound the budget from two numbers you already have: proxy
container start to metrics server listening (test 1), and container recreate latency, i.e.
`lastState.terminated.finishedAt` to `state.running.startedAt` from any of tests 7-9.

**Pass:** the sum sits at well under half the `failureThreshold` window.

**What this does and does not show.** The recreate latency above is the *workload* container's, on a
multi-GB image; the proxy is distroless and an order of magnitude smaller, so the number is
conservative rather than exact. It bounds the budget; it does not measure the proxy's own restart.

## After the run

Restart vLLM in tmux, confirm probes are succeeding again, and revert the dev-mode probe scaffolding
in the workload chart. Everything else the protocol creates lives inside the container filesystems
and disappears with the pod.

Re-run tests 2, 3 and 7 after a vLLM version bump: the design assumes vLLM does not bind its port
before its routes are serving. If a future version binds early, the budget silently drops from the
unreachable one to the reachable one during model load.
