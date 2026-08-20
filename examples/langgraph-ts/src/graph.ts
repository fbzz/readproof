// A two-node LangGraph: `load_context` mounts ctx:// resources into a Ctx
// run and commits a manifest; `answer_question` calls a chat model with the
// exact bytes that manifest recorded.
//
// The point of the example is the handoff between the two: whatever the
// model sees is pinned to `ctx_manifest_id`, which lands in the graph's
// checkpoint. Anything that can read the checkpoint later can replay the
// exact bytes of that turn — see src/replay.ts.

import type { BaseChatModel } from "@langchain/core/language_models/chat_models";
import { HumanMessage, SystemMessage } from "@langchain/core/messages";
import type { RunnableConfig } from "@langchain/core/runnables";
import { FakeListChatModel } from "@langchain/core/utils/testing";
import { Annotation, END, MemorySaver, START, StateGraph } from "@langchain/langgraph";
import { Ctx } from "@ctx/sdk";

import { CONTEXT_RESOURCES, CTX_API_KEY, CTX_ENDPOINT } from "./config.js";

/** One resource as it was delivered into this turn. */
export interface MountedEntry {
  uri: string;
  snapshot_id: string;
  content_hash: string;
  content: string;
}

/**
 * Graph state. `ctx_manifest_id` is a first-class channel rather than
 * checkpoint metadata on purpose: the id doesn't exist until the node has
 * run, and metadata is fixed when the graph is invoked. State channels are
 * written into the same checkpoint record, so a durable checkpointer
 * persists the id exactly as it persists everything else — and
 * `graph.getState(config)` hands it back.
 */
export const GraphState = Annotation.Root({
  question: Annotation<string>,
  ctx_manifest_id: Annotation<string>,
  ctx_entries: Annotation<MountedEntry[]>({
    reducer: (_previous, next) => next,
    default: () => [],
  }),
  answer: Annotation<string>,
});

export type GraphStateType = typeof GraphState.State;

/** The state key the manifest id lives under, for readers outside this file. */
export const MANIFEST_ID_KEY = "ctx_manifest_id";

function ctxClient(): Ctx {
  return new Ctx({ endpoint: CTX_ENDPOINT, apiKey: CTX_API_KEY });
}

function threadIdOf(config: RunnableConfig): string {
  const value = config.configurable?.["thread_id"];
  if (typeof value !== "string" || value === "") {
    throw new Error("no thread_id in config — invoke with { configurable: { thread_id } }");
  }
  return value;
}

/**
 * Node 1 — resolve every ctx:// resource inside one Ctx run and commit it.
 *
 * `mount()` resolves the resource *and* records it as the next ordered
 * entry of the run; `commit()` freezes those entries into an immutable
 * manifest. Content and manifest id both go into state, so the next node
 * uses exactly what was recorded — not a second, possibly different read.
 */
async function loadContext(_state: GraphStateType, config: RunnableConfig): Promise<Partial<GraphStateType>> {
  const threadId = threadIdOf(config);
  // One Ctx run per graph thread keeps the two ids trivially correlatable.
  // (Re-invoking the same thread would need a fresh run id — ctxd rejects
  // a duplicate.)
  const run = ctxClient().run({ id: `langgraph-${threadId}` });

  const entries: MountedEntry[] = [];
  for (const resource of CONTEXT_RESOURCES) {
    const resolved = await run.mount(resource.uri);
    entries.push({
      uri: resource.uri,
      snapshot_id: resolved.snapshot.id,
      content_hash: resolved.snapshot.content_hash,
      content: resolved.content,
    });
  }

  const manifest = await run.commit();
  return { ctx_manifest_id: manifest.manifest_id, ctx_entries: entries };
}

/** Node 2 — answer the question from the mounted context. */
async function answerQuestion(state: GraphStateType, _config: RunnableConfig): Promise<Partial<GraphStateType>> {
  const model = await createModel(state.ctx_entries);
  const response = await model.invoke([
    new SystemMessage(systemPrompt(state.ctx_entries)),
    new HumanMessage(state.question),
  ]);
  return { answer: response.text };
}

function systemPrompt(entries: MountedEntry[]): string {
  const docs = entries.map((e) => `<document uri="${e.uri}">\n${e.content.trim()}\n</document>`).join("\n");
  return [
    "You are a support agent. Answer only from the documents below.",
    docs,
  ].join("\n\n");
}

/**
 * Default: a fake in-memory model, so the example runs with no API key and
 * no network beyond ctxd. Its canned reply is derived from the mounted
 * policy text, which is what makes the demo legible: change the source
 * document and a fresh run's answer changes with it, while a replay of the
 * old manifest still returns the old bytes.
 *
 * Set ANTHROPIC_API_KEY and `npm install @langchain/anthropic` to swap in a
 * real model — the rest of the graph is unchanged, which is the point.
 */
async function createModel(entries: MountedEntry[]): Promise<BaseChatModel> {
  if (process.env.ANTHROPIC_API_KEY) {
    const anthropic = await loadAnthropic();
    if (anthropic) {
      return new anthropic.ChatAnthropic({
        model: process.env.CTX_ANTHROPIC_MODEL ?? "claude-opus-5",
      });
    }
    console.warn("ANTHROPIC_API_KEY is set but @langchain/anthropic is not installed — using the fake model");
  }
  return new FakeListChatModel({ responses: [cannedAnswer(entries)] });
}

function cannedAnswer(entries: MountedEntry[]): string {
  const policy = entries.find((entry) => entry.uri.endsWith("/refunds"));
  if (!policy) {
    return "I have no refund policy mounted, so I can't answer that.";
  }
  const rule = policy.content.trim().split("\n")[0] ?? "";
  return `${rule} (source: ${policy.uri}, content hash ${policy.content_hash.slice(0, 12)}…)`;
}

interface ChatAnthropicModule {
  ChatAnthropic: new (fields: { model: string }) => BaseChatModel;
}

async function loadAnthropic(): Promise<ChatAnthropicModule | null> {
  // Non-literal specifier on purpose: @langchain/anthropic is optional and
  // deliberately not in package.json, so tsc must not try to resolve it at
  // build time and Node must tolerate it being absent at runtime.
  const specifier: string = "@langchain/anthropic";
  try {
    return (await import(specifier)) as ChatAnthropicModule;
  } catch {
    return null;
  }
}

/**
 * MemorySaver is per-process, which is enough to show the mechanism: the
 * manifest id is read back out of the checkpoint via `getState`, not out of
 * the invoke() return value. Swap in SqliteSaver/PostgresSaver and the same
 * read works from a different process days later.
 */
export function buildGraph() {
  return new StateGraph(GraphState)
    .addNode("load_context", loadContext)
    // Node names and state channels share one namespace in LangGraph, so
    // this node can't just be called "answer" — that's the channel it writes.
    .addNode("answer_question", answerQuestion)
    .addEdge(START, "load_context")
    .addEdge("load_context", "answer_question")
    .addEdge("answer_question", END)
    .compile({ checkpointer: new MemorySaver() });
}
