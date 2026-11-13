// @ts-nocheck
// herdr-suite GS session-restore reporter.
//
// Single reporter rule: exactly one copy of this file belongs in the
// gentle-shell agent directory's extensions/ directory. Two copies report the
// same pane twice and race for the session identity. Install it with
// "hs gs-restore install" and undo it with "hs gs-restore remove", which puts
// back the backup taken during the install.
//
// Adapted from the herdr pi integration. A herdr reinstall or update overwrites
// the vanilla integration, so installing again is expected and idempotent.
//
// gentle-shell only: resume_argv is reported only when the gentle-shell launcher
// is resolvable from this process, which is probed once and cached. A vanilla
// pi installation reports no resume_argv, and every gentle-pi event
// subscription is skipped, so the file degrades silently there.
//
// Differences from the vanilla reporter: the session is restored through the
// gentle-shell launcher with its saved session, the session identity is
// re-reported every 5 seconds for 2 minutes, and live subagents keep the pane
// in the working state.
// HERDR_SUITE_GS_RESTORE_VERSION=1

import { spawnSync } from "node:child_process";
import net from "node:net";
import path from "node:path";

const HERDR_ENV = process.env.HERDR_ENV;
const socketPath = process.env.HERDR_SOCKET_PATH;
const socketEndpoint =
  process.platform === "win32" && socketPath ? `\\\\.\\pipe\\${socketPath}` : socketPath;
const paneId = process.env.HERDR_PANE_ID;
const source = "herdr:pi";

// gentleShellCommand is the launcher this reporter restores with, or undefined
// when the command is not resolvable. The probe runs at most once per process.
let gentleShellProbe: string | null = null;

function gentleShellCommand(): string | undefined {
  if (gentleShellProbe !== null) {
    return gentleShellProbe || undefined;
  }
  gentleShellProbe = "";
  try {
    const probe = spawnSync("gentle-shell", ["--version"], {
      stdio: "ignore",
      timeout: 3000,
    });
    if (!probe?.error && probe?.status === 0) {
      gentleShellProbe = "gentle-shell";
    }
  } catch {
    gentleShellProbe = "";
  }
  return gentleShellProbe || undefined;
}

// onBus subscribes when this pi exposes an event bus. Vanilla pi has none, so
// the callback is skipped instead of throwing during extension load.
function onBus(pi, name, handler) {
  const bus = pi?.events;
  if (!bus || typeof bus.on !== "function") {
    return;
  }
  try {
    bus.on(name, handler);
  } catch {
    return;
  }
}

function enabled() {
  return HERDR_ENV === "1" && !!socketPath && !!paneId;
}

function sendRequestAttempt(request: unknown, timeoutMs: number): Promise<boolean> {
  if (!enabled()) {
    return Promise.resolve(true);
  }

  return new Promise((resolve) => {
    let done = false;
    let timeout: ReturnType<typeof setTimeout> | undefined;
    const finish = (delivered: boolean) => {
      if (done) return;
      done = true;
      if (timeout) {
        clearTimeout(timeout);
      }
      socket.destroy();
      resolve(delivered);
    };

    const socket = net.createConnection(socketEndpoint!);
    socket.on("error", () => finish(false));
    socket.on("connect", () => socket.write(`${JSON.stringify(request)}\n`));
    socket.on("data", () => finish(true));
    socket.on("end", () => finish(false));
    timeout = setTimeout(() => finish(false), timeoutMs);
    timeout.unref?.();
  });
}

async function sendRequest(request: unknown): Promise<void> {
  if (await sendRequestAttempt(request, 500)) {
    return;
  }
  await sendRequestAttempt(request, 1500);
}

type AgentState = "working" | "blocked" | "idle";

type QueuedState = {
  state: AgentState;
  message?: string;
  seq: number;
};

let reportSeq = Date.now() * 1000;
let currentAgentSessionId: string | undefined;
let currentAgentSessionPath: string | undefined;

function nextReportSeq(): number {
  reportSeq += 1;
  return reportSeq;
}

function updateSessionRef(ctx: any): void {
  try {
    const file = ctx?.sessionManager?.getSessionFile?.();
    currentAgentSessionPath =
      typeof file === "string" &&
      (path.posix.isAbsolute(file) || path.win32.isAbsolute(file))
        ? file
        : undefined;
  } catch {
    currentAgentSessionPath = undefined;
  }

  try {
    const id = ctx?.sessionManager?.getSessionId?.();
    currentAgentSessionId = typeof id === "string" && id.length > 0 ? id : undefined;
  } catch {
    currentAgentSessionId = undefined;
  }
}

function withSessionRef(params: Record<string, unknown>): Record<string, unknown> {
  const resumeArgv = currentResumeArgv();
  if (currentAgentSessionPath) {
    return {
      ...params,
      agent_session_path: currentAgentSessionPath,
      ...(resumeArgv ? { resume_argv: resumeArgv } : {}),
    };
  }
  if (currentAgentSessionId) {
    return { ...params, agent_session_id: currentAgentSessionId };
  }
  return params;
}

function currentSessionRef(): Record<string, unknown> | undefined {
  if (currentAgentSessionPath) {
    return { agent_session_path: currentAgentSessionPath };
  }
  if (currentAgentSessionId) {
    return { agent_session_id: currentAgentSessionId };
  }
  return undefined;
}

function currentResumeArgv(): string[] | undefined {
  if (!currentAgentSessionPath) {
    return undefined;
  }
  if (!gentleShellCommand()) {
    return undefined;
  }
  return ["gentle-shell", "--", "--session", currentAgentSessionPath];
}

function reportSession(sessionStartSource?: string): Promise<void> {
  const sessionRef = currentSessionRef();
  if (!sessionRef) {
    return Promise.resolve();
  }

  return sendRequest({
    id: `${source}:session:${Date.now()}:${Math.random().toString(36).slice(2)}`,
    method: "pane.report_agent_session",
    params: {
      pane_id: paneId,
      source,
      agent: "pi",
      seq: nextReportSeq(),
      session_start_source: sessionStartSource,
      ...sessionRef,
      ...(currentResumeArgv() ? { resume_argv: currentResumeArgv() } : {}),
    },
  });
}

function sendState(state: AgentState, message?: string, seq = nextReportSeq()): Promise<void> {
  return sendRequest({
    id: `${source}:${Date.now()}:${Math.random().toString(36).slice(2)}`,
    method: "pane.report_agent",
    params: withSessionRef({
      pane_id: paneId,
      source,
      agent: "pi",
      state,
      message,
      seq,
    }),
  });
}

let sendInFlight = false;
let queuedState: QueuedState | undefined;

function queueState(state: AgentState, message?: string): void {
  queuedState = { state, message, seq: nextReportSeq() };
  if (!sendInFlight) {
    void drainStateQueue();
  }
}

async function drainStateQueue(): Promise<void> {
  if (sendInFlight) {
    return;
  }

  sendInFlight = true;
  try {
    while (queuedState) {
      const next = queuedState;
      queuedState = undefined;
      await sendState(next.state, next.message, next.seq);
    }
  } finally {
    sendInFlight = false;
    if (queuedState) {
      void drainStateQueue();
    }
  }
}

export default function (pi) {
  if (!enabled()) {
    return;
  }
  if (typeof pi?.on !== "function") {
    return;
  }

  let agentActive = false;
  let blockedCount = 0;
  let blockedMessage: string | undefined;
  let lastState: AgentState | undefined;
  let lastMessage: string | undefined;
  let rootSession = false;

  function desiredState() {
    if (blockedCount > 0) {
      return { state: "blocked" as const, message: blockedMessage };
    }
    if (agentActive || activeChildren.size > 0) {
      return { state: "working" as const, message: undefined };
    }
    return { state: "idle" as const, message: undefined };
  }

  function publishState(force = false) {
    const next = desiredState();
    if (!force && next.state === lastState && next.message === lastMessage) {
      return;
    }
    lastState = next.state;
    lastMessage = next.message;
    queueState(next.state, next.message);
  }

  onBus(pi, "herdr:blocked", (data) => {
    if (!rootSession) {
      return;
    }
    if (!data?.active) {
      blockedCount = Math.max(0, blockedCount - 1);
      if (blockedCount === 0) {
        blockedMessage = undefined;
      }
      publishState();
      return;
    }

    blockedCount += 1;
    blockedMessage = data.label;
    publishState();
  });

  const CHILD_ACTIVE_STATUSES = new Set(["queued", "running", "waiting"]);
  const CHILD_FINISHED_STATUSES = new Set(["completed", "failed", "cancelled", "timed_out"]);
  const activeChildren = new Set<string>();

  // gentle-pi publishes every subagent status change on this content-free bus.
  // Restored snapshots are silent, so a reload mid-run under-reports children
  // until their next transition.
  onBus(pi, "gentle-pi:notification:source", (data) => {
    if (!rootSession) {
      return;
    }
    const event = data as {
      source?: string;
      sessionId?: string;
      taskId?: string;
      status?: string;
    };
    if (
      event?.source !== "subagent" ||
      typeof event.taskId !== "string" ||
      event.taskId.length === 0 ||
      event.sessionId !== currentAgentSessionId
    ) {
      return;
    }
    if (CHILD_FINISHED_STATUSES.has(event.status!)) {
      activeChildren.delete(event.taskId);
    } else if (CHILD_ACTIVE_STATUSES.has(event.status!)) {
      activeChildren.add(event.taskId);
    } else {
      return;
    }
    publishState();
  });

  pi.on("session_start", async (event, ctx) => {
    // TUI only: RPC/JSON/print modes are headless (no PTY herdr can display),
    // and RPC still reports hasUI=true, so mode is the reliable gate.
    if (ctx?.mode !== "tui") {
      return;
    }
    rootSession = true;
    updateSessionRef(ctx);
    await reportSession(event?.reason);
    // Herdr drops resume_argv until the reported session identity is current;
    // identity anchors asynchronously (screen detection + server ticks), so
    // re-report every 5s for 2 minutes until one lands.
    for (let attempt = 1; attempt <= 24; attempt++) {
      const timer = setTimeout(() => {
        void reportSession(event?.reason);
      }, attempt * 5000);
      timer.unref?.();
    }
    // A reload can replace this extension mid-run without another agent_start.
    activeChildren.clear();
    agentActive = ctx?.isIdle?.() === false;
    publishState(true);
  });

  pi.on("agent_start", (_event, ctx) => {
    if (!rootSession) {
      return;
    }
    updateSessionRef(ctx);
    void reportSession();
    agentActive = true;
    publishState();
  });

  pi.on("agent_settled", (_event, ctx) => {
    if (!rootSession || ctx?.isIdle?.() !== true) {
      return;
    }

    agentActive = false;
    publishState();
  });
}
